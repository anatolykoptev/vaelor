package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/anatolykoptev/go-code/internal/freshness"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildingHealth prevents concurrent code_health builds for the same repo.
var buildingHealth sync.Map

// CodeHealthInput is the input schema for the code_health tool.
type CodeHealthInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python, rust)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope (e.g. internal/auth, pkg/api), space-separated keywords (e.g. 'auth handler'), or 'magic_numbers' for detailed magic number report"`
	Format   string `json:"format,omitempty" jsonschema_description:"Output format: 'xml' (default) or 'sarif' (SARIF v2.1.0 JSON for GitHub Code Scanning)"`
}

// registerCodeHealth registers the code_health MCP tool.
func registerCodeHealth(server *mcp.Server, cfg Config, deps analyze.Deps, semDeps *SemanticDeps, graphStore *codegraph.Store) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_health",
		Description: "Assess code quality of a single repository. " +
			"Returns grade (A-F), numeric score (0-100), metrics " +
			"(complexity, test coverage, docs, error handling, " +
			"dependency freshness, vulnerability security via OSV), " +
			"maintenance hotspots, type relationships, and prioritized " +
			"recommendations with estimated score impact. No LLM — " +
			"fast, static analysis with registry version and CVE checks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeHealthInput) (*mcp.CallToolResult, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		defer cleanup()

		// Cache-first: large repos take 30-90s and exceed the 60s MCP timeout.
		// Skip cache for sarif format (different output structure) and special focus modes.
		if graphStore != nil && input.Format != "sarif" && input.Focus == "" {
			repoKey := codegraph.GraphNameFor(root)
			_ = graphStore.EnsureHealthCacheTable(ctx)
			if cached := graphStore.LoadHealthCache(ctx, repoKey); cached != nil {
				return largeTextResult(cached.ResultXML, "code_health", outputDir), nil
			}
			// Not cached: start background computation.
			if _, alreadyBuilding := buildingHealth.LoadOrStore(repoKey, true); !alreadyBuilding {
				bgInput := input
				bgRoot := root
				bgKey := repoKey
				go func() {
					defer buildingHealth.Delete(bgKey)
					bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer bgCancel()
					if _, err := computeCodeHealth(bgCtx, bgInput, bgRoot, deps, semDeps, graphStore, outputDir); err != nil {
						slog.Warn("code_health: background computation failed",
							slog.String("repo", bgRoot), slog.Any("error", err))
					} else {
						slog.Info("code_health: background computation complete", slog.String("repo", bgRoot))
					}
				}()
			}
			return textResult(fmt.Sprintf(
				`<response tool="code_health"><status>computing</status>`+
					`<message>Health analysis started. Retry in 60 seconds — large repos take 1-2 minutes.</message>`+
					`<repo>%s</repo></response>`, input.Repo)), nil
		}

		// No graphStore, sarif format, or focus mode: run inline.
		return computeCodeHealth(ctx, input, root, deps, semDeps, graphStore, outputDir)
	})
}

// computeCodeHealth runs the full code health analysis and returns the result.
// When graphStore is non-nil and format is xml with no focus, the result is
// persisted to code_health_cache for fast subsequent lookups.
func computeCodeHealth(
	ctx context.Context,
	input CodeHealthInput,
	root string,
	deps analyze.Deps,
	semDeps *SemanticDeps,
	graphStore *codegraph.Store,
	outputDir string,
) (*mcp.CallToolResult, error) {
	// Stage 1: snapshot + special focus modes.
	sr, err := buildHealthSnapshot(ctx, root, input.Language, input.Focus)
	if err != nil {
		return errResult(fmt.Sprintf("snapshot: %s", err)), nil
	}

	if sr.isMagicMode {
		entries := compare.CollectMagicNumbers(sr.snap)
		resp := buildMagicNumbersXML(sr.snap.Name, sr.snap.Language, entries)
		return xmlMarshalResult(resp, "code_health", outputDir), nil
	}

	if sr.isSemanticDup {
		if semDeps == nil || semDeps.Store == nil {
			return errResult("semantic search not configured: set EMBED_URL and DATABASE_URL"), nil
		}
		groups := collectSemanticDupGroups(ctx, semDeps, root, sr.snap)
		resp := buildSemanticDupXML(sr.snap.Name, sr.snap.Language, groups)
		return xmlMarshalResult(resp, "code_health", outputDir), nil
	}

	// Stage 2: core metrics + semantic dup enrichment.
	metrics := compare.ComputeMetrics(sr.snap)

	// Stage 3-6: Run independent analyses in parallel.
	var wg sync.WaitGroup
	var fr healthFreshnessResult
	var hotspots []compare.HotspotFile
	var archMetrics *compare.ArchMetrics

	// Semantic duplication (optional, may be fast if no store).
	wg.Add(1)
	go func() {
		defer wg.Done()
		gatherHealthSemanticDup(ctx, semDeps, root, sr.snap, &metrics)
	}()

	// Freshness + vulnerability (HTTP calls).
	wg.Add(1)
	go func() {
		defer wg.Done()
		fr = gatherHealthFreshness(ctx, root, &metrics)
	}()

	// Hotspots (git churn).
	wg.Add(1)
	go func() {
		defer wg.Done()
		hotspots = gatherHealthHotspots(ctx, root, input.Repo, sr.snap)
	}()

	// Architecture metrics (graph DB query).
	wg.Add(1)
	go func() {
		defer wg.Done()
		archMetrics = gatherHealthArchMetrics(ctx, graphStore, root)
	}()

	// Dead code metrics (from CE scores in code_dead_code_scores).
	var deadCodeCandidates int
	var deadCodeTopNames []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		gatherHealthDeadCode(ctx, graphStore, root, &deadCodeCandidates, &deadCodeTopNames)
	}()

	// Relationship stats and outliers (CPU-bound, parallel with above).
	relStats := compare.ComputeRelStats(sr.snap.Rels)
	outliers := compare.CollectOutliers(sr.snap)

	// Wait for all parallel stages.
	wg.Wait()

	if deadCodeCandidates > 0 {
		metrics.DeadCodeCandidates = deadCodeCandidates
		metrics.DeadCodeTopNames = deadCodeTopNames
		// Recompute score/grade with dead code penalty.
		metrics.Score = compare.GradeScore(metrics)
		metrics.Grade = compare.ComputeGrade(metrics)
	}

	if input.Format == "sarif" {
		sarifReport := compare.BuildSARIF(sr.snap.Name, metrics, nil, nil, hotspots, outliers)
		data, err := json.MarshalIndent(sarifReport, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("sarif marshal: %s", err)), nil
		}
		return largeTextResult(string(data), "code_health", outputDir), nil
	}

	recs := compare.ComputeRecommendations(metrics, outliers, 5)
	oxChecks := explore.RunOxCodesHealthChecks(ctx, deps.OxCodes, root, input.Language)

	resp := buildHealthXML(sr.snap.Name, sr.snap.Language, metrics, metrics.Score, hotspots, relStats, recs, fr.fr, fr.vr, oxChecks, archMetrics)

	// Marshal to raw XML string for caching (before largeTextResult file fallback).
	data, marshalErr := xml.MarshalIndent(resp, "", "  ")
	if marshalErr != nil {
		return errResult(fmt.Sprintf("marshal: %s", marshalErr)), nil
	}
	rawXML := xml.Header + string(data)

	// Persist to cache for fast subsequent lookups (1h TTL).
	if graphStore != nil && input.Focus == "" {
		repoKey := codegraph.GraphNameFor(root)
		score, grade := extractScoreGrade(rawXML)
		if cacheErr := graphStore.UpsertHealthCache(ctx, repoKey, root, grade, rawXML, score, 3600); cacheErr != nil {
			slog.Warn("code_health: cache upsert failed", slog.String("repo", root), slog.Any("error", cacheErr))
		}
	}

	return largeTextResult(rawXML, "code_health", outputDir), nil
}

// extractScoreGrade parses score and grade from a code_health XML response string.
func extractScoreGrade(xmlStr string) (int, string) {
	score := 0
	grade := "?"
	if m := regexp.MustCompile(`score="(\d+(?:\.\d+)?)"`).FindStringSubmatch(xmlStr); len(m) > 1 {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			score = int(f)
		}
	}
	if m := regexp.MustCompile(`grade="([A-F][+]?)"`).FindStringSubmatch(xmlStr); len(m) > 1 {
		grade = m[1]
	}
	return score, grade
}

func buildHealthXML(
	name, language string,
	metrics compare.RepoMetrics,
	score float64,
	hotspots []compare.HotspotFile,
	relStats *compare.RelStats,
	recs []compare.Recommendation,
	fr *freshness.FreshnessResult,
	vr *freshness.VulnResult,
	oxChecks *explore.OxCodesHealthChecks,
	archMetrics *compare.ArchMetrics,
) xmlHealthResponse {
	resp := xmlHealthResponse{
		Health: xmlHealth{
			Repo:     name,
			Language: language,
			Metrics:  convertMetrics(metrics),
			Score:    score,
		},
	}

	if len(hotspots) > 0 {
		resp.Health.Hotspots = convertHotspots(hotspots)
	}
	if relStats != nil {
		resp.Health.RelStats = &xmlRelStats{
			Total: relStats.Total, Extends: relStats.Extends,
			Implements: relStats.Implements, Embeds: relStats.Embeds,
			UniqueSubjects: relStats.UniqueSubjects,
		}
	}
	if len(recs) > 0 {
		resp.Health.Recommendations = convertRecommendations(recs)
	}
	if fr != nil && fr.Total > 0 {
		resp.Health.DepFreshness = convertDepFreshness(fr)
	}
	if vr != nil && vr.Total > 0 {
		resp.Health.Vulnerabilities = convertVulnerabilities(vr)
	}
	if oxChecks != nil {
		resp.Health.OxChecks = convertOxCodesChecks(oxChecks)
	}
	if archMetrics != nil {
		resp.Health.ArchMetrics = convertArchMetrics(archMetrics)
	}

	return resp
}
