package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeGraphInput is the input schema for the code_graph tool.
type CodeGraphInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Query    string `json:"query" jsonschema_description:"Natural language question about the code graph (e.g. 'who calls ParseFile?', 'what depends on package store?', 'find dead code')"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit graph to files of this language (e.g. go, python)"`
	Refresh  bool   `json:"refresh,omitempty" jsonschema_description:"Force re-indexing of the graph even if cached"`
}

// registerCodeGraph registers the code_graph MCP tool.
// If store is nil the tool is not registered (DATABASE_URL not configured).
func registerCodeGraph(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	if store == nil {
		slog.Info("code_graph: DATABASE_URL not set, tool disabled")
		return
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, and coupling analysis. " +
			"Results include raw graph rows and an LLM narrative.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeGraphInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}
		if input.Query == "" {
			return errResult("query is required"), nil, nil
		}

		if !store.HasAGE(ctx) {
			return errResult("Apache AGE extension is not available in the configured PostgreSQL instance"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		isRemote := ingest.IsRemote(input.Repo)

		if input.Refresh {
			key := codegraph.GraphNameFor(root)
			if dropErr := store.DropGraph(ctx, key, key); dropErr != nil {
				slog.Warn("code_graph: drop graph failed (continuing with re-index)",
					slog.String("key", key),
					slog.Any("error", dropErr))
			}
		}

		meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, codegraph.IndexConfig{
			TTLLocal:  cfg.GraphTTLLocal,
			TTLRemote: cfg.GraphTTLRemote,
			BatchSize: cfg.GraphBatchSize,
		})
		if err != nil {
			return errResult(fmt.Sprintf("index repo: %s", err)), nil, nil
		}

		result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, meta)
		if err != nil {
			return errResult(fmt.Sprintf("query graph: %s", err)), nil, nil
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}
