package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-kit/llm"
)

// templateFreeform is the sentinel template ID used when no named template matches.
const templateFreeform = "freeform"

// GraphStats reports graph metadata attached to a query result.
type GraphStats struct {
	Vertices int  `json:"vertices"`
	Edges    int  `json:"edges"`
	Cached   bool `json:"cached"`
}

// QueryResult is the full output of a QueryGraph call.
type QueryResult struct {
	Repo       string     `json:"repo"`
	Query      string     `json:"query"`
	Template   string     `json:"template"`
	Cypher     string     `json:"cypher"`
	Results    [][]string `json:"results"`
	Narrative  string     `json:"narrative,omitempty"`
	GraphStats GraphStats `json:"graph_stats"`
}

// QueryGraph classifies a natural-language query, executes Cypher against the
// named AGE graph, and optionally generates a narrative using the LLM.
//
// Flow:
//  1. Classify(query) → template or freeform fallback
//  2. Template path: GetTemplate + Render + ExecCypher
//  3. Freeform path: GenerateCypher + ExecCypher
//  4. On freeform exec error: GenerateCypherWithRetry + retry ExecCypher
//  5. LLM narrative (non-fatal, skipped when results are empty)
func QueryGraph(ctx context.Context, store *Store, llmClient *llm.Client, graphName, query string, meta *GraphMeta) (*QueryResult, error) {
	cls, cypher, cols, err := classifyAndBuildCypher(ctx, llmClient, query)
	if err != nil {
		return nil, err
	}

	rows, cypher, err := execWithRetry(ctx, store, llmClient, graphName, query, cypher, cols, cls.Template)
	if err != nil {
		return nil, err
	}

	if rows == nil {
		rows = [][]string{}
	}

	result := &QueryResult{
		Repo:     meta.RepoPath,
		Query:    query,
		Template: cls.Template,
		Cypher:   cypher,
		Results:  rows,
		GraphStats: GraphStats{
			Vertices: meta.SymbolCount + meta.FileCount,
			Edges:    meta.EdgeCount,
			Cached:   true,
		},
	}

	// Post-process surprises template: score and rank raw edge results.
	switch cls.Template {
	case "surprises":
		result.Results, result.Narrative = PostProcessSurprises(rows, 10)
	case "graph_diff":
		result.Results, result.Narrative = postProcessGraphDiff(ctx, store, graphName, meta.RepoKey)
	case TemplateInsightDeadCode:
		// Try pre-computed scores first (populated at graph build time).
		if preScored := store.LoadDeadCodeScores(ctx, meta.RepoKey, rows); preScored != nil {
			rows = preScored
			result.Results = rows
			addNarrative(ctx, llmClient, result, rows, query, cypher)
		} else {
			// Fallback: live reranking (used on first query before build completes).
			rows = RerankDeadCode(ctx, rows)
			result.Results = rows
			addNarrative(ctx, llmClient, result, rows, query, cypher)
		}
	default:
		addNarrative(ctx, llmClient, result, rows, query, cypher)
	}
	return result, nil
}

// classifyAndBuildCypher classifies the query and generates Cypher via template or freeform.
func classifyAndBuildCypher(ctx context.Context, llmClient *llm.Client, query string) (*Classification, string, int, error) {
	cls, err := Classify(ctx, llmClient, query)
	if err != nil {
		cls = &Classification{Template: templateFreeform, Params: map[string]string{}}
	}

	var cypher string
	var cols int

	if cls.Template != templateFreeform {
		tmpl := GetTemplate(cls.Template)
		if tmpl != nil {
			cypher = tmpl.Render(cls.Params)
			cols = tmpl.Cols
		} else {
			cls.Template = templateFreeform
		}
	}

	if cls.Template == templateFreeform {
		generated, genErr := GenerateCypher(ctx, llmClient, query)
		if genErr != nil {
			return nil, "", 0, fmt.Errorf("generate cypher: %w", genErr)
		}
		cypher = generated
		cols = countReturnCols(cypher)
		slog.Info("freeform cypher generated",
			slog.String("cypher", cypher),
			slog.Int("cols", cols))
	} else {
		slog.Info("template matched",
			slog.String("template", cls.Template),
			slog.Int("cols", cols))
	}

	return cls, cypher, cols, nil
}

// execWithRetry executes Cypher, retrying once for freeform queries with self-correction.
func execWithRetry(ctx context.Context, store *Store, llmClient *llm.Client, graphName, query, cypher string, cols int, template string) ([][]string, string, error) {
	rows, execErr := store.ExecCypher(ctx, graphName, cypher, cols)
	if execErr == nil {
		return rows, cypher, nil
	}

	if template != templateFreeform {
		return nil, cypher, fmt.Errorf("cypher exec: %w", execErr)
	}

	slog.Info("freeform cypher failed, retrying with self-correction",
		slog.String("error", execErr.Error()))

	retryCypher, retryErr := GenerateCypherWithRetry(ctx, llmClient, query, execErr.Error())
	if retryErr != nil {
		return nil, cypher, fmt.Errorf("cypher failed after retry: %w (original: %w)", retryErr, execErr)
	}

	retryCols := countReturnCols(retryCypher)
	rows, execErr = store.ExecCypher(ctx, graphName, retryCypher, retryCols)
	if execErr != nil {
		return nil, retryCypher, fmt.Errorf("cypher retry exec: %w", execErr)
	}
	return rows, retryCypher, nil
}

// reReturnClause matches the RETURN ... clause in a Cypher query.
// (?s) makes . match newlines since LLM-generated Cypher may span multiple lines.
var reReturnClause = regexp.MustCompile(`(?is)\bRETURN\b\s*([\s\S]+?)(?:\bORDER\b|\bLIMIT\b|\bSKIP\b|\bUNION\b|\z)`)

// countReturnCols estimates the number of projected columns in a Cypher query
// by counting comma-separated expressions in the RETURN clause.
// Returns at least 1.
func countReturnCols(cypher string) int {
	m := reReturnClause.FindStringSubmatch(cypher)
	if len(m) < 2 {
		return 1
	}
	// Count top-level commas (not inside parentheses).
	expr := strings.TrimSpace(m[1])
	depth := 0
	cols := 1
	for _, ch := range expr {
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				cols++
			}
		}
	}
	if cols < 1 {
		cols = 1
	}
	return cols
}
