package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlGraphResponse struct {
	XMLName xml.Name      `xml:"response"`
	Graph   xmlGraphQuery `xml:"graph"`
}

type xmlGraphQuery struct {
	Repo      string       `xml:"repo,attr"`
	Template  string       `xml:"template,attr"`
	Limit     int          `xml:"limit,attr,omitempty"`
	Truncated bool         `xml:"truncated,attr,omitempty"`
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

// codeGraphStatusXML is the building/unavailable status shape returned by
// code_graph, mirroring semanticStatusXML so the caller gets a status response
// instead of a tool error.
type codeGraphStatusXML struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Query   string   `xml:"query"`
	Repo    string   `xml:"repo"`
	Status  string   `xml:"status"`
	Message string   `xml:"message"`
}

// CodeGraphInput is the input schema for the code_graph tool.
type CodeGraphInput struct {
	Repo      string            `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Query     string            `json:"query,omitempty" jsonschema:"Natural language question about the code graph (e.g. 'who calls ParseFile?', 'what depends on package store?', 'find dead code'). Required unless template is set"`
	Template  string            `json:"template,omitempty" jsonschema:"Run this query template directly instead of having the LLM classify query — faster, deterministic, and works while the LLM is unavailable (e.g. who_calls, calls_of, call_chain, dead_code). The tool description lists every template with its params"`
	Params    map[string]string `json:"params,omitempty" jsonschema:"Parameters for template, e.g. {\"name\": \"ParseFile\"} for who_calls or {\"from\": \"main\", \"to\": \"Serve\"} for call_chain; values are strings, e.g. {\"limit\": \"50\"}; limit must be a positive integer (max 500)"`
	Language  string            `json:"language,omitempty" jsonschema:"Limit graph to files of this language (e.g. go, python)"`
	Refresh   bool              `json:"refresh,omitempty" jsonschema:"Force re-indexing of the graph even if cached"`
	Narrative *bool             `json:"narrative,omitempty" jsonschema:"Set to false to skip LLM narrative generation and return only raw graph rows + Cypher (faster, fewer tokens). Default: true for query, false for template"`
}

// registerCodeGraph registers the code_graph MCP tool.
// If store is nil the tool is not registered (DATABASE_URL not configured).
func registerCodeGraph(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	if store == nil {
		slog.Info("code_graph: DATABASE_URL not set, tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO, TESTED_BY). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Lazy indexing: if the graph is not cached, it builds in the background and returns a " +
			"<status>building</status> response; retry the same query in 2-3 minutes. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, coupling analysis, " +
			"community detection (Louvain clusters — 'show communities'), " +
			"surprise scoring (hidden cross-package dependencies — 'find hidden dependencies'), " +
			"and graph diff (what changed since last rebuild — 'what changed in the graph'). " +
			"Results include raw graph rows and an LLM narrative. " +
			"Template results are capped at limit rows; truncated=\"true\" on the result means more matched — raise limit (max 500) or narrow with file/path. " +
			"To skip LLM classification pass template + params instead of (or with) query (? = optional): " +
			codegraph.TemplateSignatures() + ".",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeGraphInput) (*mcp.CallToolResult, error) {
		return handleCodeGraph(ctx, input, cfg, deps, store)
	})
}

// handleCodeGraph is the extracted handler for code_graph, callable from tests.
func handleCodeGraph(ctx context.Context, input CodeGraphInput, cfg Config, deps analyze.Deps, store *codegraph.Store) (*mcp.CallToolResult, error) {
	outputDir := cfg.OutputDir

	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	explicit, refusal := codeGraphPrecheck(&input, deps)
	if refusal != nil {
		return refusal, nil
	}

	if store != nil && !store.HasAGE(ctx) {
		return errResult("Apache AGE extension is not available in the configured PostgreSQL instance"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	isRemote := ingest.IsRemote(input.Repo)
	repoKey := codegraph.GraphNameFor(root)

	if input.Refresh {
		// Snapshot current graph before forced refresh for future diffing.
		codegraph.SnapshotBeforeRebuild(ctx, store, repoKey, repoKey)
		if dropErr := store.DropGraph(ctx, repoKey, repoKey); dropErr != nil {
			slog.Warn("code_graph: drop graph failed (continuing with re-index)",
				slog.String("key", repoKey),
				slog.Any("error", dropErr))
		}
	}

	// Check whether a fresh graph is already cached.
	// If not, launch a background goroutine to build it and return immediately
	// so the MCP client is not held for the duration of the first-time build,
	// which can take several minutes for large repos.
	indexCfg := codegraph.IndexConfig{
		TTLLocal:            cfg.GraphTTLLocal,
		TTLRemote:           cfg.GraphTTLRemote,
		BatchSize:           cfg.GraphBatchSize,
		EnableSurpriseIndex: cfg.CodegraphSurpriseIndex,
		FlowsMax:            cfg.FlowsMax,
		FlowsDFSDepth:       cfg.FlowsDFSDepth,
	}

	fresh, status := ensureAgeGraphOrStatus(ctx, "code_graph", store, root, repoKey, isRemote, indexCfg, func(status, message string) *mcp.CallToolResult {
		return textResult(buildCodeGraphStatusResponse(input, status, message))
	})
	if !fresh {
		return status, nil
	}

	meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, indexCfg)
	if err != nil {
		return errResult(fmt.Sprintf("index repo: %s", err)), nil
	}
	recordCodeGraphAge(codegraph.GraphNameFor(root), meta.BuiltAt)

	// The narrative is another LLM call (up to 15s). An explicit template is
	// the LLM-free path, so it gets no narrative unless asked for one.
	narrativeEnabled := explicit == nil
	if input.Narrative != nil {
		narrativeEnabled = *input.Narrative
	}

	result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, explicit, meta, narrativeEnabled)
	if err != nil {
		// An LLM step (classify, generate Cypher) failed — the free model
		// chain is flaky — so point the caller at the LLM-free path.
		if errors.Is(err, codegraph.ErrLLMStep) {
			return errResult(fmt.Sprintf("query graph: %s — retry with template + params to skip the LLM: %s",
				err, codegraph.TemplateSignatures())), nil
		}
		return errResult(fmt.Sprintf("query graph: %s", err)), nil
	}

	formatted, err := formatGraphXML(result)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return largeTextResult(formatted, "code_graph", outputDir), nil
}

// codeGraphPrecheck validates the request before any repo or graph work. It
// returns the caller-chosen template (nil for a natural-language query) or
// a refusal: an invalid template, a missing query, or a natural-language
// query with no LLM configured. An explicit template never needs the LLM,
// so it passes the LLM gate. Empty input.Query is filled from the template.
func codeGraphPrecheck(input *CodeGraphInput, deps analyze.Deps) (*codegraph.Classification, *mcp.CallToolResult) {
	var explicit *codegraph.Classification
	if input.Template != "" {
		cls, err := codegraph.ExplicitClassification(input.Template, input.Params)
		if err != nil {
			return nil, errResult("code_graph: " + err.Error())
		}
		explicit = cls
		if input.Query == "" {
			input.Query = codegraph.ExplicitQueryText(cls)
		}
	}
	if input.Query == "" {
		return nil, errResult("query or template is required")
	}

	// Gate: a natural-language query needs the LLM to select or generate Cypher.
	if explicit == nil && !deps.LLMHasKey {
		return nil, errResult("code_graph: natural-language queries require LLM_API_KEY to be set; " +
			"pass template + params to run without the LLM: " + codegraph.TemplateSignatures())
	}
	return explicit, nil
}

// formatGraphXML converts a QueryResult to XML string.
func formatGraphXML(result *codegraph.QueryResult) (string, error) {
	resp := xmlGraphResponse{
		Graph: xmlGraphQuery{
			Repo:      result.Repo,
			Template:  result.Template,
			Limit:     result.Limit,
			Truncated: result.Truncated,
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

	data, err := xml.Marshal(resp)
	if err != nil {
		return "", err
	}
	return xml.Header + string(data), nil
}

// buildCodeGraphStatusResponse returns an XML status response similar to
// semantic_search's buildStatusResponse, so code_graph can signal "graph is
// building" as a normal (non-error) status and include the retry hint.
func buildCodeGraphStatusResponse(input CodeGraphInput, status, message string) string {
	return xmlMarshalFragment(codeGraphStatusXML{
		Tool:    "code_graph",
		Query:   input.Query,
		Repo:    input.Repo,
		Status:  status,
		Message: message,
	})
}
