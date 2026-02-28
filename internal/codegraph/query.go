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
	// Step 1: Classify.
	cls, err := Classify(ctx, llmClient, query)
	if err != nil {
		// Total LLM failure — fall back to freeform.
		cls = &Classification{Template: templateFreeform, Params: map[string]string{}}
	}

	var cypher string
	var cols int

	// Step 2: Template path.
	if cls.Template != templateFreeform {
		tmpl := GetTemplate(cls.Template)
		if tmpl != nil {
			cypher = tmpl.Render(cls.Params)
			cols = tmpl.Cols
		} else {
			// Unknown template returned — fall through to freeform.
			cls.Template = templateFreeform
		}
	}

	// Step 3: Freeform path.
	if cls.Template == templateFreeform {
		generated, genErr := GenerateCypher(ctx, llmClient, query)
		if genErr != nil {
			return nil, fmt.Errorf("generate cypher: %w", genErr)
		}
		cypher = generated
		cols = 1 // freeform queries project one column by default
	}

	// Execute the Cypher query.
	rows, execErr := store.ExecCypher(ctx, graphName, cypher, cols)
	if execErr != nil {
		// Step 4: Retry once for freeform queries with self-correcting prompt.
		if cls.Template != templateFreeform {
			return nil, fmt.Errorf("cypher exec: %w", execErr)
		}

		slog.Info("freeform cypher failed, retrying with self-correction",
			slog.String("error", execErr.Error()))

		retryCypher, retryErr := GenerateCypherWithRetry(ctx, llmClient, query, execErr.Error())
		if retryErr != nil {
			return nil, fmt.Errorf("cypher failed after retry: %w (original: %w)", retryErr, execErr)
		}
		cypher = retryCypher
		rows, execErr = store.ExecCypher(ctx, graphName, cypher, cols)
		if execErr != nil {
			return nil, fmt.Errorf("cypher retry exec: %w", execErr)
		}
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

	// Step 5: LLM narrative (non-fatal — never fail the whole query over this).
	if llmClient != nil && len(rows) > 0 {
		rawJSON, _ := json.Marshal(rows)
		prompt := fmt.Sprintf("Question: %s\nCypher: %s\nResults:\n%s", query, cypher, string(rawJSON))
		narrative, narrativeErr := llmClient.Complete(ctx, llm.SystemPromptGraphNarrative, prompt)
		if narrativeErr == nil {
			result.Narrative = narrative
		} else {
			slog.Warn("narrative generation failed (non-fatal)", slog.Any("error", narrativeErr))
		}
	}

	return result, nil
}
