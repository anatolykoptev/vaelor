package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/biomarkers"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/mcpmeta"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// maxHealthDefaultPaths is the maximum number of hotspot paths to score
	// when no explicit paths are provided.
	maxHealthDefaultPaths = 20

	// hotspotHintThreshold is the minimum score for the top file to trigger
	// an advisory hint in the response meta.
	hotspotHintThreshold = 7
)

// defaultHealthWeights are the per-biomarker weights used by defaultHealthRegistry.
// Weights must sum to 1.0 (enforced by NewAggregator).
var defaultHealthWeights = map[string]float64{
	"prior_defect": 0.6,
	"churn_risk":   0.4,
}

// FileHealthArgs is the input schema for the get_file_health tool.
type FileHealthArgs struct {
	Repo  string   `json:"repo"  jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Paths []string `json:"paths,omitempty" jsonschema_description:"File paths (relative to repo root) to score. Defaults to top-20 hotspot files by churn."`
}

// FileHealthResult is the JSON payload returned by the get_file_health tool.
type FileHealthResult struct {
	Files []biomarkers.FileHealth `json:"files"`
	Meta  mcpmeta.Envelope        `json:"_meta"`
}

// defaultHealthRegistry returns a Registry with PriorDefect and ChurnRisk registered
// in the order expected by defaultHealthWeights.
func defaultHealthRegistry() *biomarkers.Registry {
	reg := biomarkers.NewRegistry()
	reg.Register(biomarkers.PriorDefect{})
	reg.Register(biomarkers.ChurnRisk{})
	return reg
}

// topHotspotPaths collects churn data for repo root and returns the top-max
// paths ranked by ChurnScore descending. Returns nil, nil when the repo has
// no git history (CollectChurn returns empty map without error).
func topHotspotPaths(ctx context.Context, repo string, max int) ([]string, error) {
	churn, err := compare.CollectChurn(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("collect churn: %w", err)
	}
	if len(churn) == 0 {
		return nil, nil
	}

	type entry struct {
		path  string
		score float64
	}
	entries := make([]entry, 0, len(churn))
	for path, stats := range churn {
		entries = append(entries, entry{path: path, score: stats.ChurnScore()})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		return entries[i].path < entries[j].path
	})

	if max > 0 && len(entries) > max {
		entries = entries[:max]
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.path
	}
	return paths, nil
}

// handleFileHealthCore is the testable core of the get_file_health tool.
func handleFileHealthCore(ctx context.Context, args FileHealthArgs, agg *biomarkers.Aggregator, cfg Config, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if args.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, args.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	t0 := time.Now()

	paths := args.Paths
	if len(paths) == 0 {
		var herr error
		paths, herr = topHotspotPaths(ctx, root, maxHealthDefaultPaths)
		if herr != nil {
			return errResult(fmt.Sprintf("collect churn: %s", herr)), nil
		}
	}

	out := FileHealthResult{}
	for _, p := range paths {
		fh, serr := agg.ScoreFile(ctx, root, p)
		if serr != nil {
			fh = biomarkers.FileHealth{
				Path:    p,
				Score:   0,
				Reasons: map[string]string{"error": serr.Error()},
				Raw:     map[string]float64{},
			}
		}
		out.Files = append(out.Files, fh)
	}

	sort.SliceStable(out.Files, func(i, j int) bool {
		if out.Files[i].Score != out.Files[j].Score {
			return out.Files[i].Score > out.Files[j].Score
		}
		return out.Files[i].Path < out.Files[j].Path
	})

	// Build advisory hint from top file — references only our own response
	// keys (prior_defect, churn_risk), never tool names or arg keys that
	// could be wrong or non-existent.
	var hint string
	if len(out.Files) > 0 && out.Files[0].Score >= hotspotHintThreshold {
		top := out.Files[0]
		hint = fmt.Sprintf(
			"top file %s scored %d/10 — review the reasons map for prior_defect / churn_risk drivers",
			top.Path, top.Score,
		)
	}

	out.Meta = mcpmeta.Wrap(time.Since(t0), hint)

	body, merr := json.MarshalIndent(out, "", "  ")
	if merr != nil {
		return errResult(fmt.Sprintf("marshal: %s", merr)), nil
	}
	return textResult(string(body)), nil
}

// registerFileHealth registers the get_file_health tool on the MCP server.
// The aggregator is built once at registration time — NewAggregator panics on
// bad weight config, so panicking here (at server startup) is intentional and
// gives clear diagnostic rather than panicking inside a request handler.
func registerFileHealth(server *mcp.Server, cfg Config, deps analyze.Deps) {
	reg := defaultHealthRegistry()
	agg := biomarkers.NewAggregator(reg, defaultHealthWeights)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "get_file_health",
		Description: "Report a 1-10 health score per file using prior_defect + churn_risk biomarkers. Optional paths defaults to top-20 hotspots.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FileHealthArgs) (*mcp.CallToolResult, error) {
		return handleFileHealthCore(ctx, args, agg, cfg, deps)
	})
}
