package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/anatolykoptev/go-code/internal/freshness"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
		return xmlMarshalResult(resp, "code_health", outputDir), nil
	})
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
