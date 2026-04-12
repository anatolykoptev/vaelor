package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/anatolykoptev/go-kit/llm"
)

// postProcessSurprises scores raw cross-package edge rows and returns top-N
// with a narrative summary. Input cols: fromName, fromFile, fromCommunity,
// toName, toFile, toCommunity, fromPageRank, toPageRank.
func postProcessSurprises(rows [][]string, limit int) ([][]string, string) {
	if limit <= 0 {
		limit = 10
	}

	var edges []surpriseEdge
	for _, row := range rows {
		if len(row) < 8 {
			continue
		}
		e := surpriseEdge{
			FromName: row[0], FromFile: row[1],
			ToName: row[3], ToFile: row[4],
			EdgeLabel: "CALLS",
		}
		e.FromPkg = pkgFromFile(row[1])
		e.ToPkg = pkgFromFile(row[4])
		e.FromCommunity = atoiSafe(row[2])
		e.ToCommunity = atoiSafe(row[5])
		e.FromPageRank = atofSafe(row[6])
		e.ToPageRank = atofSafe(row[7])
		edges = append(edges, e)
	}

	results := rankSurprises(edges, limit)

	var out [][]string
	for _, r := range results {
		out = append(out, []string{
			r.FromName, r.FromFile,
			r.ToName, r.ToFile,
			fmt.Sprintf("%d", r.Score),
			strings.Join(r.Reasons, "; "),
		})
	}

	narrative := fmt.Sprintf("Found %d surprising connections out of %d cross-file edges.", len(results), len(rows))
	return out, narrative
}

// pkgFromFile extracts the package directory from a relative file path.
func pkgFromFile(relFile string) string {
	idx := strings.LastIndex(relFile, "/")
	if idx < 0 {
		return "."
	}
	return relFile[:idx]
}

// atoiSafe parses an int from an AGE agtype string, returning 0 on failure.
func atoiSafe(s string) int {
	s = strings.Trim(s, `"`)
	v, _ := strconv.Atoi(s)
	return v
}

// atofSafe parses a float from an AGE agtype string, returning 0 on failure.
func atofSafe(s string) float64 {
	s = strings.Trim(s, `"`)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// postProcessGraphDiff loads the latest snapshot, builds current state from AGE,
// computes a diff, and returns formatted rows + narrative.
func postProcessGraphDiff(ctx context.Context, store *Store, graphName, repoKey string) ([][]string, string) {
	snap, err := loadLatestSnapshot(ctx, store, repoKey)
	if err != nil {
		return nil, fmt.Sprintf("error loading snapshot: %s", err)
	}
	if snap == nil {
		return nil, "No previous snapshot found. Run with refresh=true to create a baseline, then query graph_diff after the next rebuild."
	}

	currentSnap, err := buildSnapshotFromAGE(ctx, store, graphName)
	if err != nil {
		return nil, fmt.Sprintf("error reading current graph: %s", err)
	}

	diff := DiffGraphs(*snap, currentSnap)

	var rows [][]string
	for _, s := range diff.AddedSymbols {
		rows = append(rows, []string{"+ symbol", s.Name, s.Kind, s.File})
	}
	for _, s := range diff.RemovedSymbols {
		rows = append(rows, []string{"- symbol", s.Name, s.Kind, s.File})
	}
	for _, e := range diff.AddedEdges {
		rows = append(rows, []string{"+ edge", e.From, e.Label, e.To})
	}
	for _, e := range diff.RemovedEdges {
		rows = append(rows, []string{"- edge", e.From, e.Label, e.To})
	}
	for _, m := range diff.CommunityMigrations {
		rows = append(rows, []string{"~ community", m.Name, fmt.Sprintf("%d → %d", m.OldCommunity, m.NewCommunity), m.File})
	}
	for _, c := range diff.ComplexityChanges {
		rows = append(rows, []string{"~ complexity", c.Name, fmt.Sprintf("%d → %d", c.OldComplexity, c.NewComplexity), c.File})
	}

	return rows, diff.Summary
}

// addNarrative generates an LLM narrative for the query results (non-fatal).
func addNarrative(ctx context.Context, llmClient *llm.Client, result *QueryResult, rows [][]string, query, cypher string) {
	if llmClient == nil || len(rows) == 0 {
		return
	}
	rawJSON, err := json.Marshal(rows)
	if err != nil {
		slog.Warn("narrative: marshal results failed", slog.Any("error", err))
		return
	}
	prompt := fmt.Sprintf("Question: %s\nCypher: %s\nResults:\n%s", query, cypher, string(rawJSON))
	narrative, err := llmClient.Complete(ctx, prompts.SystemPromptGraphNarrative, prompt)
	if err == nil {
		result.Narrative = narrative
	} else {
		slog.Warn("narrative generation failed (non-fatal)", slog.Any("error", err))
	}
}
