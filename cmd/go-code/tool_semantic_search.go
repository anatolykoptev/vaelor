package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SemanticSearchInput is the input schema for the semantic_search tool.
type SemanticSearchInput struct {
	Repo     string `json:"repo" jsonschema_description:"GitHub repo (owner/repo) or local path to search in"`
	Query    string `json:"query" jsonschema_description:"Natural language description of what you're looking for (e.g. 'function that validates JWT tokens', 'error handling for database connections')"`
	Language string `json:"language,omitempty" jsonschema_description:"Filter by language (e.g. go, python, typescript)"`
	TopK     int    `json:"top_k,omitempty" jsonschema_description:"Number of results (default 10, max 50)"`
}

// SemanticDeps holds dependencies for semantic search.
type SemanticDeps struct {
	Client      *embeddings.Client
	Store       *embeddings.Store
	Pipeline    *embeddings.Pipeline
	AnalyzeDeps analyze.Deps
}

const (
	defaultSemanticTopK = 10
	maxSemanticTopK     = 50
)

// registerSemanticSearch registers the semantic_search MCP tool.
func registerSemanticSearch(server *mcp.Server, _ Config, deps SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "semantic_search",
		Description: "Find code by meaning using natural language queries. " +
			"Searches function and method bodies using vector similarity (embeddings). " +
			"Works best after the repository has been indexed via code_graph or repo_analyze. " +
			"Returns ranked results with file paths, symbol names, and similarity scores.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SemanticSearchInput) (*mcp.CallToolResult, error) {
		return handleSemanticSearch(ctx, input, deps)
	})
}

func handleSemanticSearch(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if deps.Client == nil || deps.Store == nil {
		return textResult(buildDisabledResponse(input)), nil
	}

	topK := input.TopK
	if topK <= 0 {
		topK = defaultSemanticTopK
	}
	if topK > maxSemanticTopK {
		topK = maxSemanticTopK
	}

	// Resolve repo root.
	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps.AnalyzeDeps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	repoKey := codegraph.GraphNameFor(root)

	// Embed query first (fast, ~1s).
	vector, err := deps.Client.EmbedQuery(ctx, input.Query)
	if err != nil {
		return errResult(fmt.Sprintf("embed query: %s", err)), nil
	}

	// Try searching existing embeddings.
	results, err := deps.Store.Search(ctx, vector, embeddings.SearchOpts{
		RepoKey:  repoKey,
		Language: input.Language,
		TopK:     topK,
	})
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	if len(results) > 0 {
		// Found results — trigger background re-index for freshness, return immediately.
		if deps.Pipeline != nil {
			deps.Pipeline.IndexRepoAsync(repoKey, root)
		}
		return textResult(formatSemanticResults(input, results)), nil
	}

	// No results — start background indexing if not already running.
	if deps.Pipeline != nil {
		if deps.Pipeline.IsIndexing(repoKey) {
			return textResult(buildIndexingResponse(input)), nil
		}
		deps.Pipeline.IndexRepoAsync(repoKey, root)
		return textResult(buildIndexingResponse(input)), nil
	}

	return textResult(buildNotIndexedResponse(input)), nil
}

func formatSemanticResults(input SemanticSearchInput, results []embeddings.SearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"semantic_search\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "    <result rank=\"%d\" distance=\"%.4f\">\n", i+1, r.Distance)
		fmt.Fprintf(&sb, "      <file>%s</file>\n", escapeXML(r.FilePath))
		fmt.Fprintf(&sb, "      <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "      <line>%d</line>\n", r.StartLine)
		fmt.Fprintf(&sb, "      <language>%s</language>\n", escapeXML(r.Language))
		fmt.Fprintf(&sb, "    </result>\n")
	}
	sb.WriteString("  </results>\n</response>")
	return sb.String()
}

func buildDisabledResponse(input SemanticSearchInput) string {
	return fmt.Sprintf(
		"<response tool=\"semantic_search\">\n"+
			"  <query>%s</query>\n"+
			"  <repo>%s</repo>\n"+
			"  <status>disabled</status>\n"+
			"  <message>Semantic search is not available: embedding service not configured. "+
			"Set EMBED_URL and EMBED_MODEL environment variables to enable.</message>\n"+
			"</response>",
		escapeXML(input.Query), escapeXML(input.Repo))
}

func buildIndexingResponse(input SemanticSearchInput) string {
	return fmt.Sprintf(
		"<response tool=\"semantic_search\">\n"+
			"  <query>%s</query>\n"+
			"  <repo>%s</repo>\n"+
			"  <status>indexing</status>\n"+
			"  <message>Repository is being indexed in the background. "+
			"This may take a few minutes for the first run. "+
			"Please retry in 30-60 seconds.</message>\n"+
			"</response>",
		escapeXML(input.Query), escapeXML(input.Repo))
}

func buildNotIndexedResponse(input SemanticSearchInput) string {
	return fmt.Sprintf(
		"<response tool=\"semantic_search\">\n"+
			"  <query>%s</query>\n"+
			"  <repo>%s</repo>\n"+
			"  <status>not_indexed</status>\n"+
			"  <message>No indexed code found and embedding pipeline is not configured. "+
			"Ensure EMBED_URL is set and retry.</message>\n"+
			"</response>",
		escapeXML(input.Query), escapeXML(input.Repo))
}
