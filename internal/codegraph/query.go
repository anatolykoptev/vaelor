package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/llm"
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

	addNarrative(ctx, llmClient, result, rows, query, cypher)
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
		cols = 1
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

	rows, execErr = store.ExecCypher(ctx, graphName, retryCypher, cols)
	if execErr != nil {
		return nil, retryCypher, fmt.Errorf("cypher retry exec: %w", execErr)
	}
	return rows, retryCypher, nil
}

// addNarrative generates an LLM narrative for the query results (non-fatal).
func addNarrative(ctx context.Context, llmClient *llm.Client, result *QueryResult, rows [][]string, query, cypher string) {
	if llmClient == nil || len(rows) == 0 {
		return
	}
	rawJSON, _ := json.Marshal(rows)
	prompt := fmt.Sprintf("Question: %s\nCypher: %s\nResults:\n%s", query, cypher, string(rawJSON))
	narrative, err := llmClient.Complete(ctx, llm.SystemPromptGraphNarrative, prompt)
	if err == nil {
		result.Narrative = narrative
	} else {
		slog.Warn("narrative generation failed (non-fatal)", slog.Any("error", err))
	}
}
