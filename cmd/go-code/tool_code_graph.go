package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildingRepos tracks repos currently being indexed to prevent concurrent builds.
var buildingRepos sync.Map

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

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO, TESTED_BY). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, coupling analysis, " +
			"community detection (Louvain clusters — 'show communities'), " +
			"surprise scoring (hidden cross-package dependencies — 'find hidden dependencies'), " +
			"and graph diff (what changed since last rebuild — 'what changed in the graph'). " +
			"Results include raw graph rows and an LLM narrative.",
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
	if input.Query == "" {
		return errResult("query is required"), nil
	}

	// Gate: code_graph NL-query requires LLM to generate or select Cypher.
	if !deps.LLMHasKey {
		return errResult("code_graph: requires LLM_API_KEY to be set"), nil
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

	if input.Refresh {
		key := codegraph.GraphNameFor(root)
		// Snapshot current graph before forced refresh for future diffing.
		codegraph.SnapshotBeforeRebuild(ctx, store, key, key)
		if dropErr := store.DropGraph(ctx, key, key); dropErr != nil {
			slog.Warn("code_graph: drop graph failed (continuing with re-index)",
				slog.String("key", key),
				slog.Any("error", dropErr))
		}
	}

	// Check whether a fresh graph is already cached.
	// If not, launch a background goroutine to build it and return immediately
	// so the MCP client is not held for the duration of the first-time build,
	// which can take several minutes for large repos.
	fresh, cacheErr := codegraph.CacheStatus(ctx, store, root)
	if cacheErr != nil {
		slog.Warn("code_graph: cache status check failed, proceeding with sync build",
			slog.Any("error", cacheErr))
	}

	indexCfg := codegraph.IndexConfig{
		TTLLocal:            cfg.GraphTTLLocal,
		TTLRemote:           cfg.GraphTTLRemote,
		BatchSize:           cfg.GraphBatchSize,
		EnableSurpriseIndex: cfg.CodegraphSurpriseIndex,
		FlowsMax:            cfg.FlowsMax,
		FlowsDFSDepth:       cfg.FlowsDFSDepth,
	}

	if !fresh {
		// Not cached: build in background and tell the client to retry.
		// Use sync.Map to prevent two concurrent goroutines building the same graph
		// (AGE is not concurrency-safe for writes to the same graph).
		repoKey := codegraph.GraphNameFor(root)
		if _, alreadyBuilding := buildingRepos.LoadOrStore(repoKey, true); alreadyBuilding {
			return errResult("graph is being built — retry in 2-3 minutes"), nil
		}
		bgRoot := root
		go func() {
			defer buildingRepos.Delete(repoKey)
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer bgCancel()
			if bgMeta, err := codegraph.IndexRepo(bgCtx, store, bgRoot, isRemote, indexCfg); err != nil {
				recordCodeGraphBuildFailure(err)
				slog.Warn("code_graph: background index failed",
					slog.String("repo", bgRoot), slog.Any("error", err))
			} else {
				recordCodeGraphAge(repoKey, bgMeta.BuiltAt)
				slog.Info("code_graph: background index complete", slog.String("repo", bgRoot))
			}
		}()
		return errResult("graph is being built — retry in 2-3 minutes"), nil
	}

	meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, indexCfg)
	if err != nil {
		return errResult(fmt.Sprintf("index repo: %s", err)), nil
	}
	recordCodeGraphAge(codegraph.GraphNameFor(root), meta.BuiltAt)

	result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, meta)
	if err != nil {
		return errResult(fmt.Sprintf("query graph: %s", err)), nil
	}

	formatted, err := formatGraphXML(result)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return largeTextResult(formatted, "code_graph", outputDir), nil
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
