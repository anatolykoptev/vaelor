package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/anatolykoptev/go-kit/rerank"
)

const (
	rerankModel         = "gte-multi-rerank"
	rerankDeadCodeQuery = "orphaned function with no callers that is a bug risk"
	rerankTopN          = 20
	// rerankPreFilterN is the max docs sent to reranker; ~4s for 20 docs on ARM.
	rerankPreFilterN = 20
	// defaultRerankTimeout bounds a dead_code rerank call (~0.2s/doc on ARM).
	defaultRerankTimeout = 35 * time.Second
)

var rerankTimeout = defaultRerankTimeout // set in init via parseTimeoutSecs

// rerankClient is the shared cross-encoder client (go-kit/rerank). It is
// configured from EMBED_URL + EMBED_TOKEN — the same embedding server used for
// semantic_search — so dead_code and semantic rerank reach whatever host
// EMBED_URL points at (local embed-server or the embed.krolik.tools edge),
// authenticated via the EMBED_TOKEN bearer.
//
// Timeout is left at 0 so each caller bounds the call with its own ctx deadline
// (dead_code 35s, semantic 15s). When EMBED_URL is empty the client reports
// Available()==false and callers skip reranking, preserving byte-identical
// output (cold-path guarantee).
var rerankClient *rerank.Client

func init() {
	rerankTimeout = parseTimeoutSecs("GOCODE_RERANK_TIMEOUT_S", defaultRerankTimeout)
	rerankClient = rerank.New(rerank.Config{
		URL:    os.Getenv("EMBED_URL"),
		Model:  rerankModel,
		APIKey: os.Getenv("EMBED_TOKEN"),
		// MaxDocs sizes to the build-time path's upper bound (it sends ALL
		// candidates, up to maxOrphanCandidates). Runtime callsites pre-filter
		// to rerankPreFilterN themselves, so a larger cap never truncates them.
		MaxDocs: maxOrphanCandidates,
	}, slog.Default())
}

// RerankDeadCode reranks dead_code Cypher rows by likelihood of being
// actual dead code (not entrypoints or test utilities).
//
// Pre-filters to top rerankPreFilterN rows by complexity before calling the
// reranker — gte-multi-rerank takes ~0.2s/doc on ARM, so 20 docs ≈ 4s total.
//
// Returns the top rerankTopN rows sorted by relevance_score DESC.
// Falls back to original rows (capped at rerankTopN) when the reranker is not
// configured or any error occurs (non-fatal).
func RerankDeadCode(_ context.Context, rows [][]string) [][]string {
	if len(rows) == 0 {
		return rows
	}

	// Pre-filter: sort by complexity DESC and take top rerankPreFilterN.
	// High-complexity orphan functions are the most interesting dead code candidates.
	type rowWithComplexity struct {
		row        []string
		complexity int
	}
	candidates := make([]rowWithComplexity, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		cx := parseIntField(row[0], "complexity")
		candidates = append(candidates, rowWithComplexity{row, cx})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].complexity > candidates[j].complexity
	})
	limit := rerankPreFilterN
	if len(candidates) < limit {
		limit = len(candidates)
	}
	candidates = candidates[:limit]

	candidateRows := make([][]string, len(candidates))
	for i, c := range candidates {
		candidateRows[i] = c.row
	}

	// Cold-path guarantee: with no reranker configured, return the original
	// (complexity-sorted) order capped at rerankTopN — byte-identical to the
	// pre-rerank behaviour.
	if !rerankClient.Available() {
		return capRows(candidateRows, rerankTopN)
	}

	// Build documents for the reranker.
	docs := make([]rerank.Doc, len(candidates))
	for i, c := range candidates {
		docs[i] = rerank.Doc{Text: formatDeadCodeDoc(c.row[0])}
	}

	// Use a background context so the reranker call is not bound by the
	// MCP request context (which may be near its 60s deadline already).
	rerankCtx, cancel := context.WithTimeout(context.Background(), rerankTimeout)
	defer cancel()

	// RerankWithResult returns docs sorted by relevance DESC on success; on a
	// degraded/skipped call it returns the input order unchanged (Score=0,
	// OrigRank=i), so the fallback below is the original complexity order capped
	// at rerankTopN.
	res, _ := rerankClient.RerankWithResult(rerankCtx, rerankDeadCodeQuery, docs)
	if res == nil {
		return capRows(candidateRows, rerankTopN)
	}
	scored := res.Scored

	limit = min(rerankTopN, len(scored))
	reranked := make([][]string, 0, limit)
	for _, s := range scored[:limit] {
		if s.OrigRank >= 0 && s.OrigRank < len(candidateRows) {
			reranked = append(reranked, candidateRows[s.OrigRank])
		}
	}

	topScore := 0.0
	if len(scored) > 0 {
		topScore = float64(scored[0].Score)
	}

	slog.Info("codegraph: dead_code reranked",
		slog.Int("input", len(rows)),
		slog.Int("output", len(reranked)),
		slog.Float64("top_score", topScore))

	return reranked
}

// capRows returns rows truncated to at most n entries.
func capRows(rows [][]string, n int) [][]string {
	if len(rows) > n {
		return rows[:n]
	}
	return rows
}

// formatDeadCodeDoc extracts key fields from an AGE vertex JSON string
// and formats them as a human-readable document for the reranker.
func formatDeadCodeDoc(vertexJSON string) string {
	name := extractFieldRerank(vertexJSON, "name")
	file := extractFieldRerank(vertexJSON, "file")
	complexity := extractFieldRerank(vertexJSON, "complexity")
	sig := extractFieldRerank(vertexJSON, "signature")

	var sb strings.Builder
	if name != "" {
		fmt.Fprintf(&sb, "name: %s", name)
	}
	if file != "" {
		fmt.Fprintf(&sb, ", file: %s", file)
	}
	if complexity != "" {
		fmt.Fprintf(&sb, ", complexity: %s", complexity)
	}
	if sig != "" {
		// Truncate long signatures.
		if len(sig) > 100 {
			sig = sig[:100] + "..."
		}
		fmt.Fprintf(&sb, ", signature: %s", sig)
	}
	if sb.Len() == 0 {
		maxLen := 120
		if len(vertexJSON) < maxLen {
			maxLen = len(vertexJSON)
		}
		return vertexJSON[:maxLen]
	}
	return sb.String()
}

// extractFieldRerank extracts a JSON string field value from an AGE vertex string.
// AGE vertex format: {"id":..., "properties":{"name":"...", "file":"..."}}
// Named extractFieldRerank to avoid collision with any future extractField in the package.
func extractFieldRerank(s, field string) string {
	key := `"` + field + `":`
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(s[idx+len(key):])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end <= 0 {
		return ""
	}
	return rest[:end]
}

// parseIntField parses an integer field from an AGE vertex JSON string.
func parseIntField(s, field string) int {
	val := extractFieldRerank(s, field)
	if val == "" {
		return 0
	}
	n := 0
	for _, c := range val {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
