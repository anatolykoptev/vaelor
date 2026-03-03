package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlGraphResponse struct {
	XMLName xml.Name      `xml:"response"`
	Graph   xmlGraphQuery `xml:"graph"`
}

type xmlGraphQuery struct {
	Repo      string       `xml:"repo,attr"`
	Template  string       `xml:"template,attr"`
	Vertices  int          `xml:"vertices,attr"`
	Edges     int          `xml:"edges,attr"`
	Cached    bool         `xml:"cached,attr"`
	Query     string       `xml:"query"`
	Cypher    string       `xml:"cypher"`
	Results   xmlGraphRows `xml:"results"`
	Narrative string       `xml:"narrative,omitempty"`
}

type xmlGraphRows struct {
	Rows []xmlGraphRow `xml:"row"`
}

type xmlGraphRow struct {
	Cols []string `xml:"col"`
}

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

	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, and coupling analysis. " +
			"Results include raw graph rows and an LLM narrative.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeGraphInput) (*mcp.CallToolResult, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}
		if input.Query == "" {
			return errResult("query is required"), nil
		}

		if !store.HasAGE(ctx) {
			return errResult("Apache AGE extension is not available in the configured PostgreSQL instance"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
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
			return errResult(fmt.Sprintf("index repo: %s", err)), nil
		}

		result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, meta)
		if err != nil {
			return errResult(fmt.Sprintf("query graph: %s", err)), nil
		}

		formatted, err := formatGraphXML(result)
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil
		}

		return largeTextResult(formatted, "code_graph", outputDir), nil
	})
}

// formatGraphXML converts a QueryResult to XML string.
func formatGraphXML(result *codegraph.QueryResult) (string, error) {
	resp := xmlGraphResponse{
		Graph: xmlGraphQuery{
			Repo:      result.Repo,
			Template:  result.Template,
			Vertices:  result.GraphStats.Vertices,
			Edges:     result.GraphStats.Edges,
			Cached:    result.GraphStats.Cached,
			Query:     result.Query,
			Cypher:    result.Cypher,
			Narrative: result.Narrative,
		},
	}
	rows := make([]xmlGraphRow, len(result.Results))
	for i, r := range result.Results {
		rows[i] = xmlGraphRow{Cols: r}
	}
	resp.Graph.Results = xmlGraphRows{Rows: rows}

	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(data), nil
}
