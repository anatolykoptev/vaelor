package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-kit/embed"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SemanticSearchInput is the input schema for the semantic_search tool.
type SemanticSearchInput struct {
	Repo        string  `json:"repo" jsonschema_description:"GitHub repo (owner/repo) or local path to search in"`
	Query       string  `json:"query" jsonschema_description:"Natural language description of what you're looking for (e.g. 'function that validates JWT tokens', 'error handling for database connections')"`
	Language    string  `json:"language,omitempty" jsonschema_description:"Filter by language (e.g. go, python, typescript)"`
	TopK        int     `json:"top_k,omitempty" jsonschema_description:"Number of results (default 10, max 50)"`
	MaxDistance float32 `json:"max_distance,omitempty" jsonschema_description:"Maximum cosine distance (0.0-1.0, default 0.75). Lower = stricter matching"`
}

// SemanticDeps holds dependencies for semantic search.
type SemanticDeps struct {
	Client      *embed.Client
	Store       *embeddings.Store
	Pipeline    *embeddings.Pipeline
	AnalyzeDeps analyze.Deps
	Expander    *embeddings.Expander
	OxCodes     *oxcodes.Client
	// RRFWeights are the per-retriever weights threaded into MergeRRF.
	// Defaults to (1.0, 1.0) — byte-identical to unweighted RRF.
	RRFWeights embeddings.RRFWeights
}

const (
	defaultSemanticTopK      = 10
	maxSemanticTopK          = 50
	// semanticRerankCandidates is the minimum candidate pool for CE reranker.
	// Ensures at least 20 candidates regardless of topK.
	semanticRerankCandidates = 20
)

// registerSemanticSearch registers the semantic_search MCP tool.
func registerSemanticSearch(server *mcp.Server, _ Config, deps SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "semantic_search",
		Description: "Find code by meaning using natural language queries. " +
			"Uses hybrid RRF (keyword + vector similarity) with 1-hop graph expansion via Apache AGE. " +
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
		return textResult(buildStatusResponse(input, "disabled",
			"Semantic search is not available: embedding service not configured. "+
				"Set EMBED_URL and EMBED_MODEL environment variables to enable.")), nil
	}

	topK := input.TopK
	if topK <= 0 {
		topK = defaultSemanticTopK
	}
	if topK > maxSemanticTopK {
		topK = maxSemanticTopK
	}

	maxDist := input.MaxDistance
	if maxDist <= 0 {
		maxDist = 0.85 // CE reranker filters noise; higher threshold improves recall
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
		RepoKey:     repoKey,
		Language:    input.Language,
		TopK:        topK,
		MaxDistance: maxDist,
	})
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	if len(results) > 0 {
		return handleSemanticHits(ctx, input, deps, repoKey, root, results, topK, maxDist)
	}

	// No results — start background indexing if not already running.
	if deps.Pipeline != nil {
		if deps.Pipeline.IsIndexing(repoKey) {
			done, total, _ := deps.Pipeline.IndexProgress(repoKey)
			msg := "Repository is being indexed in the background. Please retry in 30-60 seconds."
			if total > 0 {
				msg = fmt.Sprintf("Indexing in progress: %d/%d symbols embedded. Please retry in 30-60 seconds.", done, total)
			}
			return textResult(buildStatusResponse(input, "indexing", msg)), nil
		}
		deps.Pipeline.IndexRepoAsync(repoKey, root)
		return textResult(buildStatusResponse(input, "indexing",
			"Repository indexing started in the background. "+
				"Please retry in 30-60 seconds.")), nil
	}

	return textResult(buildStatusResponse(input, "not_indexed",
		"No indexed code found and embedding pipeline is not configured. "+
			"Ensure EMBED_URL is set and retry.")), nil
}

// handleSemanticHits handles the path where semantic search returned results:
// graph expansion, hybrid keyword merge via RRF, and final formatting.
func handleSemanticHits(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, results []embeddings.SearchResult, topK int, maxDist float32,
) (*mcp.CallToolResult, error) {
	// Trigger background re-index for freshness.
	if deps.Pipeline != nil {
		deps.Pipeline.IndexRepoAsync(repoKey, root)
	}

	// Graph expansion: add 1-hop CALLS neighbors before hybrid merge
	// so graph-expanded symbols can participate in RRF naturally.
	if deps.Expander != nil {
		const maxGraphExtra = 5
		extra := deps.Expander.Expand(ctx, repoKey, results, maxGraphExtra)
		results = append(results, extra...)
	}

	// Symbol name search: find functions whose names contain query keywords.
	// Fills recall gaps where vector distance misses well-named private functions.
	if deps.Store != nil {
		kws := embeddings.ExtractQueryKeywords(input.Query)
		if nameHits, nerr := deps.Store.SearchBySymbolName(ctx, repoKey, kws, input.Language, 20); nerr == nil {
			results = append(results, nameHits...)
		}
	}

	// Hybrid: run keyword search and merge with RRF.
	// Overretrieve before CE reranking so the reranker sees more candidates.
	rerankCap := max(topK*2, semanticRerankCandidates)
	var keyHits []embeddings.FileLineHit
	if scopedHits := runScopedKeywordSearch(ctx, deps.OxCodes, input.Query, root, input.Language); len(scopedHits) > 0 {
		keyHits = scopedHits
	} else {
		keyHits = runKeywordSearch(ctx, input.Query, root)
	}
	if len(keyHits) > 0 {
		matched, matchErr := deps.Store.MatchKeywordHits(ctx, repoKey, keyHits)
		if matchErr == nil && len(matched) > 0 {
			// Merge with an enlarged pool so CE reranker can pick the best topK.
			hybrid := embeddings.MergeRRF(results, matched, rerankCap, deps.RRFWeights)
			// Flatten HybridResult → SearchResult for CE reranker.
			flat := make([]embeddings.SearchResult, len(hybrid))
			for i, h := range hybrid {
				flat[i] = h.SearchResult
				flat[i].Source = h.Source
			}
			// CE reranking: reorder by cross-encoder relevance score.
			reranked := codegraph.RerankSemanticResults(ctx, root, input.Query, flat, topK)
			// Annotate with PageRank for architectural awareness.
			reranked = annotateWithPageRank(ctx, reranked, deps.AnalyzeDeps.Graph, root)
			return textResult(formatSemanticResults(input, reranked, deps.AnalyzeDeps.PathMappings)), nil
		}
	}
	// Fallback to pure semantic — filter by distance (graph results have Distance=1.0).
	filtered := make([]embeddings.SearchResult, 0, len(results))
	for _, r := range results {
		if maxDist > 0 && r.Distance >= maxDist {
			continue
		}
		filtered = append(filtered, r)
	}
	// CE reranking on pure semantic fallback path.
	reranked := codegraph.RerankSemanticResults(ctx, root, input.Query, filtered, topK)
	// Annotate with PageRank for architectural awareness.
	reranked = annotateWithPageRank(ctx, reranked, deps.AnalyzeDeps.Graph, root)
	return textResult(formatSemanticResults(input, reranked, deps.AnalyzeDeps.PathMappings)), nil
}

// annotateWithPageRank adds PageRank signals to results for architectural awareness.
// Non-fatal: results without PageRank data keep zero value and are not shown in output.
func annotateWithPageRank(ctx context.Context, results []embeddings.SearchResult, graph graphx.Analytics, repoKey string) []embeddings.SearchResult {
	if graph == nil || len(results) == 0 {
		return results
	}

	// Single batch query for top-200 PageRank symbols (same pattern as sortCallersByPageRank).
	const batchSize = 200
	signals, err := graph.TopPageRank(ctx, repoKey, batchSize)
	if err != nil || len(signals) == 0 {
		return results
	}

	prMap := make(map[string]float64, len(signals))
	for _, sig := range signals {
		key := sig.Symbol.File + ":" + sig.Symbol.Name
		prMap[key] = sig.PageRank
	}

	annotated := make([]embeddings.SearchResult, len(results))
	copy(annotated, results)
	for i, r := range annotated {
		if r.SymbolName == "" {
			continue
		}
		if pr, ok := prMap[r.FilePath+":"+r.SymbolName]; ok {
			annotated[i].PageRank = float32(pr)
		}
	}
	return annotated
}

func formatSemanticResults(input SemanticSearchInput, results []embeddings.SearchResult, mappings []analyze.PathMapping) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"semantic_search\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(results))
	for i, r := range results {
		source := r.Source
		if source == "" {
			source = "semantic"
		}
		if r.PageRank > 0 {
			fmt.Fprintf(&sb, "    <result rank=\"%d\" distance=\"%.4f\" source=\"%s\" pagerank=\"%.6f\">\n",
				i+1, r.Distance, escapeXML(source), r.PageRank)
		} else {
			fmt.Fprintf(&sb, "    <result rank=\"%d\" distance=\"%.4f\" source=\"%s\">\n", i+1, r.Distance, escapeXML(source))
		}
		fmt.Fprintf(&sb, "      <file>%s</file>\n", escapeXML(reverseToHost(r.FilePath, mappings)))
		fmt.Fprintf(&sb, "      <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "      <line>%d</line>\n", r.StartLine)
		fmt.Fprintf(&sb, "      <language>%s</language>\n", escapeXML(r.Language))
		fmt.Fprintf(&sb, "    </result>\n")
	}
	sb.WriteString("  </results>\n</response>")
	return sb.String()
}

func buildStatusResponse(input SemanticSearchInput, status, message string) string {
	return fmt.Sprintf(
		"<response tool=\"semantic_search\">\n"+
			"  <query>%s</query>\n"+
			"  <repo>%s</repo>\n"+
			"  <status>%s</status>\n"+
			"  <message>%s</message>\n"+
			"</response>",
		escapeXML(input.Query), escapeXML(input.Repo), status, message)
}
