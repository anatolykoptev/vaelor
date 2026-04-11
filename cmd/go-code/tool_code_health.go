package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/anatolykoptev/go-code/internal/freshness"
	"github.com/anatolykoptev/go-code/internal/semhealth"
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

		// Determine snapshot focus — magic_numbers and semantic_duplicates are special modes, not path filters.
		snapshotFocus := input.Focus
		isMagicMode := input.Focus == "magic_numbers"
		if isMagicMode || input.Focus == "semantic_duplicates" {
			snapshotFocus = ""
		}

		snap, err := compare.BuildSnapshot(ctx, root, compare.SnapshotOpts{
			Focus:    snapshotFocus,
			Language: input.Language,
		})
		if err != nil {
			return errResult(fmt.Sprintf("snapshot: %s", err)), nil
		}

		// Magic numbers focused report.
		if isMagicMode {
			entries := compare.CollectMagicNumbers(snap)
			resp := buildMagicNumbersXML(snap.Name, snap.Language, entries)
			return xmlMarshalResult(resp, "code_health", outputDir), nil
		}

		// Semantic duplicates focused report.
		if input.Focus == "semantic_duplicates" {
			if semDeps == nil || semDeps.Store == nil {
				return errResult("semantic search not configured: set EMBED_URL and DATABASE_URL"), nil
			}
			repoKey := codegraph.GraphNameFor(root)
			funcCount := countFuncs(snap.Symbols)
			sem := semhealth.Analyze(ctx, semDeps.Store, repoKey, funcCount)
			var groups []semhealth.DupGroup
			if sem != nil {
				groups = sem.DupGroups
			}
			resp := buildSemanticDupXML(snap.Name, snap.Language, groups)
			return xmlMarshalResult(resp, "code_health", outputDir), nil
		}

		metrics := compare.ComputeMetrics(snap)
		score := compare.GradeScore(metrics)

		// Semantic duplication analysis (optional, non-fatal).
		if semDeps != nil && semDeps.Store != nil {
			repoKey := codegraph.GraphNameFor(root)
			funcCount := countFuncs(snap.Symbols)
			if sem := semhealth.Analyze(ctx, semDeps.Store, repoKey, funcCount); sem != nil && sem.SemanticDupRatio > 0 {
				metrics.SemanticDupRatio = sem.SemanticDupRatio
				score = compare.GradeScore(metrics)
				metrics.Score = score
				metrics.Grade = compare.ComputeGrade(metrics)
			}
		}

		// Dependency freshness and vulnerability checks (optional, non-fatal).
		var fr *freshness.FreshnessResult
		var vr *freshness.VulnResult
		manifests := freshness.DiscoverManifests(root)
		if len(manifests) > 0 {
			freshnessTimeout := 30 * time.Second
			client := &http.Client{Timeout: freshnessTimeout}
			allDeps := freshness.CollectDeps(manifests)

			if len(allDeps) > 0 {
				// Freshness check.
				reg := freshness.NewMultiRegistryWithCache(client, nil)
				fr = freshness.CheckFreshness(ctx, allDeps, reg)
				metrics.DepFreshnessRatio = fr.Ratio

				// Vulnerability check.
				vr = freshness.CheckVulnerabilities(ctx, allDeps, client, freshness.DefaultOSVURL)
				metrics.VulnSecurityRatio = vr.Ratio
			}

			// Go runtime version check.
			for _, m := range manifests {
				if m.Language == "go" && m.RuntimeVersion != "" {
					status := freshness.CheckGoRuntime(ctx, client, m.RuntimeVersion)
					if fr == nil {
						fr = &freshness.FreshnessResult{Ratio: 1.0}
					}
					fr.RuntimeStatus = status
					break
				}
			}

			// Recompute score after freshness/vuln updates.
			score = compare.GradeScore(metrics)
			metrics.Score = score
			metrics.Grade = compare.ComputeGrade(metrics)
		}

		// Hotspot analysis (non-fatal).
		churn, churnErr := compare.CollectChurn(ctx, root)
		if churnErr != nil {
			slog.Debug("code_health: churn collection failed", slog.String("repo", input.Repo), slog.Any("error", churnErr))
		}
		var hotspots []compare.HotspotFile
		if churn != nil {
			hotspots = compare.ComputeHotspots(churn, compare.FileComplexityFromSnapshot(snap))
		}

		relStats := compare.ComputeRelStats(snap.Rels)

		// Recommendations.
		outliers := compare.CollectOutliers(snap)

		// SARIF output for GitHub Code Scanning.
		if input.Format == "sarif" {
			sarifReport := compare.BuildSARIF(snap.Name, metrics, nil, nil, hotspots, outliers)
			data, err := json.MarshalIndent(sarifReport, "", "  ")
			if err != nil {
				return errResult(fmt.Sprintf("sarif marshal: %s", err)), nil
			}
			return largeTextResult(string(data), "code_health", outputDir), nil
		}

		recs := compare.ComputeRecommendations(metrics, outliers, 5)

		// Ox-codes quality checks (informational, non-fatal, do not affect grade).
		oxChecks := explore.RunOxCodesHealthChecks(ctx, deps.OxCodes, root, input.Language)

		// Architecture metrics from graph store (optional, non-fatal).
		var archMetrics *compare.ArchMetrics
		if graphStore != nil {
			gctx, gcancel := context.WithTimeout(ctx, 30*time.Second)
			defer gcancel()
			archMetrics = compare.CollectArchMetrics(gctx, graphStore, root)
		}

		resp := buildHealthXML(snap.Name, snap.Language, metrics, score, hotspots, relStats, recs, fr, vr, oxChecks, archMetrics)
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
