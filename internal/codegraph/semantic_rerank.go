package codegraph

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/go-kit/rerank"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

const (
	semanticRerankTopN = 20 // max docs sent to reranker
	codeSignatureLines = 5  // lines to read for CE context
	// defaultSemanticRerankTimeout bounds a semantic_search CE rerank call.
	defaultSemanticRerankTimeout = 15 * time.Second
)

var semanticRerankTimeout = defaultSemanticRerankTimeout // set in init via parseTimeoutSecs

func init() {
	semanticRerankTimeout = parseTimeoutSecs("GOCODE_SEMANTIC_RERANK_TIMEOUT_S", defaultSemanticRerankTimeout)
}

// readCodeSignature reads the first nLines lines from a source file starting
// at startLine. Returns empty string on any error — CE falls back gracefully.
func readCodeSignature(root, relFilePath string, startLine, nLines int) string {
	if root == "" || relFilePath == "" || startLine <= 0 {
		return ""
	}
	fullPath := filepath.Join(root, relFilePath)
	f, err := os.Open(fullPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var lines []string
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}
		lines = append(lines, scanner.Text())
		if len(lines) >= nLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// RerankSemanticResults applies CE reranking to semantic search results.
// Takes the merged results from hybrid RRF, sends top semanticRerankTopN to
// gte-multi-rerank, returns reranked in order of relevance (highest first).
//
// Falls back to original order on any error (non-fatal, logs warning).
// The caller's topK is applied AFTER reranking so the best topK are returned.
func RerankSemanticResults(
	ctx context.Context,
	root string,
	query string,
	results []embeddings.SearchResult,
	topK int,
) []embeddings.SearchResult {
	if len(results) == 0 || query == "" {
		return results
	}

	// Defensive dedup: collapse FilePath+":"+SymbolName duplicates before any
	// output path (CE rerank, cold-path cappedResults, context-cancelled cap).
	// The hybrid path deduplicates via MergeRRF; the semantic-only path via
	// semanticOnlyResult. This layer hardens against phantom rows from Bug B
	// (stale AGE index) or any future upstream path that bypasses those dedup
	// points. Keeps lowest Distance. Key form = MergeRRF (rrf.go:98).
	results = dedupByFileSymbol(results, "ce_rerank")

	// Bail early if the inbound context is already cancelled.
	if err := ctx.Err(); err != nil {
		return cappedResults(results, topK)
	}

	// Cold-path guarantee: with no reranker configured, return the original
	// order (now deduped) capped at topK.
	if !rerankClient.Available() {
		return cappedResults(results, topK)
	}

	// Cap input: send at most semanticRerankTopN to the reranker.
	candidates := results
	if len(candidates) > semanticRerankTopN {
		candidates = candidates[:semanticRerankTopN]
	}

	docs := buildSemanticDocs(root, candidates)

	// Use a background context so the reranker call is not bound by the
	// MCP request context (which may be near its 60s deadline already).
	rerankCtx, cancel := context.WithTimeout(context.Background(), semanticRerankTimeout)
	defer cancel()

	// RerankWithResult returns docs sorted by relevance DESC on success. On a
	// degraded/skipped call, fall back to the original order WITHOUT relabelling
	// results as "ce_reranked" — the provenance must not claim a rerank that did
	// not happen (matches the pre-migration error path).
	res, _ := rerankClient.RerankWithResult(rerankCtx, query, docs)
	if res == nil || (res.Status != rerank.StatusOk && res.Status != rerank.StatusFallback) {
		return cappedResults(results, topK)
	}
	scored := res.Scored

	// Build reranked result list. OrigRank maps each scored doc back to its
	// candidate index.
	out := make([]embeddings.SearchResult, 0, min(topK, len(scored)))
	for _, s := range scored {
		if s.OrigRank < 0 || s.OrigRank >= len(candidates) {
			continue
		}
		res := candidates[s.OrigRank]
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

// cappedResults returns results truncated to at most topK entries.
func cappedResults(results []embeddings.SearchResult, topK int) []embeddings.SearchResult {
	if topK < len(results) {
		return results[:topK]
	}
	return results
}

// dedupByFileSymbol deduplicates results by FilePath+":"+SymbolName, keeping
// the entry with the lowest Distance (best cosine match) and preserving
// relative order. Bumps RecordSemanticDupCollapsed(path) per dropped entry.
// Key form matches MergeRRF (internal/embeddings/rrf.go:98).
func dedupByFileSymbol(results []embeddings.SearchResult, path string) []embeddings.SearchResult {
	seen := make(map[string]int, len(results)) // key → index in out
	out := make([]embeddings.SearchResult, 0, len(results))
	for _, r := range results {
		key := r.FilePath + ":" + r.SymbolName
		if idx, ok := seen[key]; ok {
			if r.Distance < out[idx].Distance {
				out[idx] = r
			}
			RecordSemanticDupCollapsed(path)
			continue
		}
		seen[key] = len(out)
		out = append(out, r)
	}
	return out
}

// buildSemanticDocs formats each search result as a reranker document, including
// a short code signature (first codeSignatureLines lines from StartLine) so the
// cross-encoder can distinguish same-named symbols in different files/contexts.
func buildSemanticDocs(root string, candidates []embeddings.SearchResult) []rerank.Doc {
	docs := make([]rerank.Doc, len(candidates))
	for i, r := range candidates {
		var sb strings.Builder
		fmt.Fprintf(&sb, "symbol: %s", r.SymbolName)
		if r.SymbolKind != "" {
			fmt.Fprintf(&sb, " (%s)", r.SymbolKind)
		}
		if r.FilePath != "" {
			fmt.Fprintf(&sb, "\nfile: %s", r.FilePath)
		}
		if r.Language != "" {
			fmt.Fprintf(&sb, "\nlanguage: %s", r.Language)
		}
		sig := readCodeSignature(root, r.FilePath, r.StartLine, codeSignatureLines)
		if sig != "" {
			fmt.Fprintf(&sb, "\ncode:\n%s", sig)
		}
		docs[i] = rerank.Doc{Text: sb.String()}
	}
	return docs
}
