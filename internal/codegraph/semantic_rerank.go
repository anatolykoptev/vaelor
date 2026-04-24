package codegraph

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const (
	semanticRerankTimeout = 15 * time.Second
	semanticRerankTopN    = 20 // max docs sent to reranker
	codeSignatureLines    = 5  // lines to read for CE context
)

var semanticRerankClient = &http.Client{Timeout: semanticRerankTimeout}

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
	// Includes code signature (first codeSignatureLines lines from StartLine)
	// so CE can distinguish same-named symbols in different files/contexts.
	docs := make([]string, len(candidates))
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
		// Include code signature for richer CE context.
		sig := readCodeSignature(root, r.FilePath, r.StartLine, codeSignatureLines)
		if sig != "" {
			fmt.Fprintf(&sb, "\ncode:\n%s", sig)
		}
		docs[i] = sb.String()
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
