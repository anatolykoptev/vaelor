package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const (
	semanticRerankTimeout = 15 * time.Second
	semanticRerankTopN    = 20 // max docs sent to reranker
)

var semanticRerankClient = &http.Client{Timeout: semanticRerankTimeout}

// RerankSemanticResults applies CE reranking to semantic search results.
// Takes the merged results from hybrid RRF, sends top semanticRerankTopN to
// gte-multi-rerank, returns reranked in order of relevance (highest first).
//
// Falls back to original order on any error (non-fatal, logs warning).
// The caller's topK is applied AFTER reranking so the best topK are returned.
func RerankSemanticResults(
	ctx context.Context,
	query string,
	results []embeddings.SearchResult,
	topK int,
) []embeddings.SearchResult {
	if len(results) == 0 || query == "" {
		return results
	}

	// Bail early if the inbound context is already cancelled.
	if err := ctx.Err(); err != nil {
		if topK < len(results) {
			return results[:topK]
		}
		return results
	}

	// Cap input: send at most semanticRerankTopN to the reranker.
	candidates := results
	if len(candidates) > semanticRerankTopN {
		candidates = candidates[:semanticRerankTopN]
	}

	// Format each result as a document for the CE model.
	// Format: "symbol: FuncName (kind)\nfile: path/to/file.go\nlanguage: go"
	// CE sees (query, document) holistically — even without code body,
	// symbol name + file path gives strong signal.
	docs := make([]string, len(candidates))
	for i, r := range candidates {
		doc := fmt.Sprintf("symbol: %s", r.SymbolName)
		if r.SymbolKind != "" {
			doc += fmt.Sprintf(" (%s)", r.SymbolKind)
		}
		if r.FilePath != "" {
			doc += fmt.Sprintf("\nfile: %s", r.FilePath)
		}
		if r.Language != "" {
			doc += fmt.Sprintf("\nlanguage: %s", r.Language)
		}
		docs[i] = doc
	}

	// Use a background context so the reranker call is not bound by the
	// MCP request context (which may be near its 60s deadline already).
	rerankCtx, cancel := context.WithTimeout(context.Background(), semanticRerankTimeout)
	defer cancel()

	scored, err := callRerankerWithClient(rerankCtx, semanticRerankClient, query, docs)
	if err != nil {
		slog.Warn("semantic_search: CE rerank failed, using original order",
			slog.Any("error", err))
		if topK < len(results) {
			return results[:topK]
		}
		return results
	}

	// Sort by CE relevance score DESC.
	sort.Slice(scored.Results, func(i, j int) bool {
		return scored.Results[i].RelevanceScore > scored.Results[j].RelevanceScore
	})

	// Build reranked result list.
	out := make([]embeddings.SearchResult, 0, min(topK, len(scored.Results)))
	for _, r := range scored.Results {
		if r.Index >= len(candidates) {
			continue
		}
		res := candidates[r.Index]
		res.Source = "ce_reranked"
		out = append(out, res)
		if len(out) >= topK {
			break
		}
	}

	slog.Info("semantic_search: CE reranked",
		slog.Int("input", len(candidates)),
		slog.Int("output", len(out)))

	return out
}
