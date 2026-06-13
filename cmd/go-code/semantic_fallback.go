package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const (
	semanticFallbackTopK = 5
)

// symbolNameSearcher is the minimal interface that semanticSuggest needs from
// the embedding store. *embeddings.Store satisfies it via SearchBySymbolName.
// Extracted here so tests can wire a spy without a live Postgres pool.
//
// Why an interface here rather than on SemanticDeps.Store: Store is a concrete
// type used across many callers; narrowing to an interface at the consumer
// (semantic_fallback.go) avoids touching the embedding package contract and
// mirrors the SimilarPairFinder pattern from internal/semhealth/semhealth.go.
type symbolNameSearcher interface {
	SearchBySymbolName(ctx context.Context, repoKey string, keywords []string, language string, limit int) ([]embeddings.SearchResult, error)
}

// Compile-time assertion: *embeddings.Store satisfies symbolNameSearcher.
// Catches signature drift on Store.SearchBySymbolName at build time rather
// than at runtime when the trigram fallback path first fires.
var _ symbolNameSearcher = (*embeddings.Store)(nil)

// bm25Searcher is the minimal interface that runKeywordArm needs from the
// embedding store when KeywordArm == "bm25f". *embeddings.Store satisfies it
// via BM25Search (BM25F P3). Extracted to allow test spies without a live pool.
type bm25Searcher interface {
	BM25Search(ctx context.Context, repoKey, queryText, language string, topK int) ([]embeddings.KeywordHit, error)
}

// Compile-time assertion: *embeddings.Store satisfies bm25Searcher.
var _ bm25Searcher = (*embeddings.Store)(nil)

// modelChecker is the minimal interface that the stale-hit guard in
// handleSemanticSearch needs. Production wires (*embeddings.Store).GetStoredModel
// for the store side and (*embeddings.Pipeline).EmbedModel + InvalidateIfModelChanged
// for the pipeline side. Extracted as a seam so tests inject fakes without live Postgres.
type modelChecker interface {
	// GetStoredModel returns the embed_model stored in code_repo_state for the
	// given repo_key, or "" on miss/error.
	GetStoredModel(ctx context.Context, repoKey string) string
}

// perRowModelChecker is an optional extension of modelChecker that reads the
// embed_model directly from code_embeddings rows rather than from code_repo_state.
// It fires when GetStoredModel returns "" (no state row — e.g. orphan vectors from
// a removed checkout that were never purged). *embeddings.Store satisfies both
// interfaces; test fakes that only set up modelChecker are unchanged.
type perRowModelChecker interface {
	// GetEmbedModelForRepo returns the embed_model of any stored embedding row for
	// the given repo_key, or "" when no rows exist or on error.
	GetEmbedModelForRepo(ctx context.Context, repoKey string) string
}

// pipelineInvalidator is the subset of *embeddings.Pipeline needed by the
// stale-hit guard: reading the active model name and triggering a purge+reindex.
type pipelineInvalidator interface {
	EmbedModel() string
	InvalidateIfModelChanged(ctx context.Context, repoKey string) bool
	IsIndexing(repoKey string) bool
	IndexRepoAsyncWithTool(tool, repoKey, root string) bool
	IndexProgress(repoKey string) (done, total int, running bool)
}

// Compile-time assertions: concrete types satisfy the seam interfaces.
var _ modelChecker = (*embeddings.Store)(nil)
var _ perRowModelChecker = (*embeddings.Store)(nil)
var _ pipelineInvalidator = (*embeddings.Pipeline)(nil)

// semanticSuggest runs a trigram fuzzy name match as fallback when the primary
// tool found no symbol. Uses pg_trgm on code_embeddings.symbol_name (GIN
// trigram index) — independent of the embed-server / jina worker availability.
//
// Why pg_trgm and not embedding: callers here typo on symbol names ("render_coturn"
// missing → suggest "render_xray", "render_caddy"). Substring/typo similarity is
// the correct primitive; semantic embedding is overkill and was the source of
// 5-90s queue-overflow hangs on a saturated jina worker (incident 2026-05-27).
//
// Returns formatted XML suggestions, or empty string on no result / nil deps /
// unindexed repo (degrades silently, same as before).
func semanticSuggest(ctx context.Context, sem *SemanticDeps, root, query, language string) string {
	if sem == nil {
		return ""
	}

	// Resolve the searcher: prefer the test-injected spy, fall back to the
	// concrete Store. Nil on both paths → unindexed / unconfigured → degrade.
	var searcher symbolNameSearcher
	if sem.storeSearcher != nil {
		searcher = sem.storeSearcher
	} else if sem.Store != nil {
		searcher = sem.Store
	}
	if searcher == nil {
		return ""
	}

	repoKey := codegraph.GraphNameFor(root)

	results, err := searcher.SearchBySymbolName(ctx, repoKey, []string{query}, language, semanticFallbackTopK)
	if err != nil {
		slog.Debug("trigram fallback: search failed", slog.Any("error", err))
		return ""
	}
	if len(results) == 0 {
		return ""
	}
	return formatSemanticSuggestions(results)
}

// formatSemanticSuggestions formats semantic search results as XML suggestions block.
func formatSemanticSuggestions(results []embeddings.SearchResult) string {
	var sb strings.Builder
	sb.WriteString("\n<semantic_suggestions>\n")
	sb.WriteString("  <hint>No exact matches found. These symbols are semantically similar to your query:</hint>\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "  <suggestion rank=\"%d\" distance=\"%.4f\">\n", i+1, r.Distance)
		fmt.Fprintf(&sb, "    <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "    <file line=\"%d\">%s</file>\n", r.StartLine, escapeXML(r.FilePath))
		sb.WriteString("  </suggestion>\n")
	}
	sb.WriteString("</semantic_suggestions>")
	return sb.String()
}
