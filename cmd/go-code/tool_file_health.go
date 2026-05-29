package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

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
)

// defaultHealthWeights are the per-biomarker weights used by defaultHealthRegistry.
// Weights must sum to 1.0 (enforced by NewAggregator).
var defaultHealthWeights = map[string]float64{
	"prior_defect": 0.6,
	"churn_risk":   0.4,
}

// FileHealthArgs is the input schema for the get_file_health tool.
type FileHealthArgs struct {
	Repo  string   `json:"repo"  jsonschema_description:"Repository absolute local path"`
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
		// Non-fatal: non-git dirs return an error from git; treat as empty.
		return nil, nil //nolint:nilerr
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
		return entries[i].score > entries[j].score
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
func handleFileHealthCore(ctx context.Context, args FileHealthArgs) (*mcp.CallToolResult, error) {
	if args.Repo == "" {
		return errResult("repo: required"), nil
	}

	t0 := time.Now()

	reg := defaultHealthRegistry()
	agg := biomarkers.NewAggregator(reg, defaultHealthWeights)

	paths := args.Paths
	if len(paths) == 0 {
		var err error
		paths, err = topHotspotPaths(ctx, args.Repo, maxHealthDefaultPaths)
		if err != nil {
			return errResult(fmt.Sprintf("collect churn: %s", err)), nil
		}
	}

	files := make([]biomarkers.FileHealth, 0, len(paths))
	for _, p := range paths {
		fh, err := agg.ScoreFile(ctx, args.Repo, p)
		if err != nil {
			return errResult(fmt.Sprintf("score file %q: %s", p, err)), nil
		}
		files = append(files, fh)
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Score > files[j].Score
	})

	// Build hint from top file.
	var hint string
	if len(files) > 0 && files[0].Score >= 7 {
		hint = fmt.Sprintf("%s scored %d — call understand(path=...) or get_dead_code(path=...)",
			files[0].Path, files[0].Score)
	}

	out := FileHealthResult{
		Files: files,
		Meta:  mcpmeta.Wrap(time.Since(t0), hint),
	}

	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(body)), nil
}

// handleFileHealth adapts handleFileHealthCore to the mcp.ToolHandler signature.
func handleFileHealth(ctx context.Context, _ *mcp.CallToolRequest, input FileHealthArgs) (*mcp.CallToolResult, error) {
	return handleFileHealthCore(ctx, input)
}

// registerFileHealth registers the get_file_health tool on the MCP server.
func registerFileHealth(server *mcp.Server) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "get_file_health",
		Description: "Report a 1-10 health score per file using prior_defect + churn_risk biomarkers. Optional paths defaults to top-20 hotspots.",
	}, handleFileHealth)
}
