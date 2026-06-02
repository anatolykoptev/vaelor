package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/mcpmeta"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/sparse"
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
	// Defaults to (1.0, 1.0, 0.0) — Sparse is dark-launched at 0.0 (P4).
	RRFWeights embeddings.RRFWeights
	// SparseClient is the SPLADE sparse embedder used for query-time retrieval
	// (P4 dark-launch). Nil when SPARSE_EMBED_URL is unset — arm is bypassed
	// entirely, yielding byte-identical behavior to the 2-arm baseline.
	SparseClient sparse.SparseEmbedder
	// KeywordArm selects the lexical retriever for the Keyword slot of MergeRRF.
	// "grep" (default) → byte-identical to pre-BM25F behavior.
	// "bm25f" → BM25F over trigram-prefiltered candidates (BM25F P4 dark-launch).
	// runKeywordArm() reads this field and falls back to grep on bm25f error.
	KeywordArm string
	// storeSearcher is the interface used by semanticSuggest for trigram name
	// lookup. Production leaves this nil and semanticSuggest falls back to Store.
	// Tests wire a spy here to avoid a real Postgres connection.
	storeSearcher symbolNameSearcher
	// bm25searcher is the BM25Search test seam. Production leaves this nil and
	// runKeywordArm falls back to Store. Tests wire a spy to avoid a live pool.
	bm25searcher bm25Searcher
}

const (
	defaultSemanticTopK = 10
	maxSemanticTopK     = 50
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

	t0 := time.Now()

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
		return handleSemanticHits(ctx, input, deps, repoKey, root, results, topK, maxDist, t0)
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
	t0 time.Time,
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

	// Hybrid: run keyword search and (P4 dark-launch) sparse retrieval, then
	// merge all arms with 3-way weighted RRF.
	// Overretrieve before CE reranking so the reranker sees more candidates.
	rerankCap := max(topK*2, semanticRerankCandidates)

	// Keyword arm: flag-gated (KEYWORD_ARM=grep|bm25f, default grep).
	// runKeywordArm returns []KeywordHit ready for MergeRRF — no MatchKeywordHits
	// needed when bm25f supplies hits directly (it already maps to KeywordHit).
	// grep path still returns FileLineHit and requires MatchKeywordHits (below).
	keyHits, matched := runKeywordArm(ctx, deps, input.Query, repoKey, root, input.Language, rerankCap)

	// SPLADE sparse arm (P4 dark-launch): nil client → no DB hit, empty slice.
	// Failure inside SearchSparse is logged + counter-bumped there; we always
	// get back a (possibly empty) slice — never an error we must handle here.
	// Empty sparse arm + weight 0.0 → byte-identical 2-arm output (guaranteed
	// by WeightedRRF math, verified by TestMergeRRF_EmptySparseArmIdentical).
	var sparseHits []embeddings.SparseHit
	if deps.SparseClient != nil && deps.Store != nil {
		sparseHits, _ = deps.Store.SearchSparse(ctx, input.Query, deps.SparseClient, embeddings.SearchOpts{
			RepoKey:  repoKey,
			Language: input.Language,
			TopK:     rerankCap,
		})
	}

	// grep path: FileLineHit → KeywordHit via MatchKeywordHits (DB symbol resolve).
	// bm25f path: already KeywordHit, keyHits is nil.
	if len(keyHits) > 0 && deps.Store != nil {
		if resolved, err := deps.Store.MatchKeywordHits(ctx, repoKey, keyHits); err == nil {
			matched = resolved
		}
	}

	if len(matched) > 0 || len(sparseHits) > 0 {
		return hybridResult(ctx, input, deps, repoKey, root, results, matched, sparseHits, rerankCap, topK, t0)
	}
	return semanticOnlyResult(ctx, input, deps, repoKey, root, results, topK, maxDist, t0)
}

// hybridResult runs the hybrid RRF merge → CE rerank → annotate → format pipeline.
// Called when at least one non-semantic arm (keyword or sparse) produced hits.
func hybridResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, semantic []embeddings.SearchResult,
	matched []embeddings.KeywordHit, sparse []embeddings.SparseHit,
	rerankCap, topK int, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Merge with an enlarged pool so CE reranker can pick the best topK.
	hybrid := embeddings.MergeRRF(semantic, matched, sparse, rerankCap, deps.RRFWeights)
	// Flatten HybridResult → SearchResult for CE reranker.
	flat := make([]embeddings.SearchResult, len(hybrid))
	for i, h := range hybrid {
		flat[i] = h.SearchResult
		flat[i].Source = h.Source
	}
	return finalResult(ctx, input, deps, repoKey, root, flat, topK, t0)
}

// semanticOnlyResult filters by distance then applies CE rerank → annotate → format.
// Called when keyword and sparse arms yielded no hits.
//
// MergeRRF (the hybrid path) deduplicates by FilePath+":"+SymbolName. This
// path skips MergeRRF, so both the dense-cosine arm and the trigram-name arm
// (appended by handleSemanticHits via SearchBySymbolName) can return the same
// symbol at different distances. We dedup here using the same key form, keeping
// the entry with the lowest Distance (best match) and preserving relative order.
func semanticOnlyResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, results []embeddings.SearchResult,
	topK int, maxDist float32, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Fallback to pure semantic — filter by distance (graph results have Distance=1.0).
	// Dedup by FilePath+":"+SymbolName, keeping the lowest Distance (best match).
	// Key form matches MergeRRF (internal/embeddings/rrf.go:98).
	seen := make(map[string]int, len(results)) // key → index in filtered
	filtered := make([]embeddings.SearchResult, 0, len(results))
	for _, r := range results {
		if maxDist > 0 && r.Distance >= maxDist {
			continue
		}
		key := r.FilePath + ":" + r.SymbolName
		if idx, ok := seen[key]; ok {
			// Already in filtered — keep the lower Distance (better cosine match).
			if r.Distance < filtered[idx].Distance {
				filtered[idx] = r
			}
			codegraph.RecordSemanticDupCollapsed("semantic_only")
			continue
		}
		seen[key] = len(filtered)
		filtered = append(filtered, r)
	}
	return finalResult(ctx, input, deps, repoKey, root, filtered, topK, t0)
}

// finalResult runs CE reranking → PageRank annotation → freshness wrap → format.
// Shared terminal step for both the hybrid and semantic-only paths.
func finalResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, candidates []embeddings.SearchResult,
	topK int, t0 time.Time,
) (*mcp.CallToolResult, error) {
	reranked := codegraph.RerankSemanticResults(ctx, root, input.Query, candidates, topK)
	reranked = annotateWithPageRank(ctx, reranked, deps.AnalyzeDeps.Graph, root)
	hint := mcpmeta.HintAfterCodeSearch(input.Query, len(reranked), symbolNameFromResults(reranked))
	env := mcpmeta.Wrap(time.Since(t0), hint)
	if sha := deps.AnalyzeDeps.IndexedSHA(ctx, repoKey); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	return metaResult(formatSemanticResults(input, reranked, deps.AnalyzeDeps.PathMappings), env), nil
}

// symbolNameFromResults returns the symbol name from the first result when there
// is exactly one result, or "" otherwise. Used to build calibrated hints.
func symbolNameFromResults(results []embeddings.SearchResult) string {
	if len(results) == 1 {
		return results[0].SymbolName
	}
	return ""
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
