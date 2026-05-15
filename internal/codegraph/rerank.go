package codegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	rerankURL           = "http://embed-server:8082/v1/rerank"
	rerankModel         = "gte-multi-rerank"
	rerankDeadCodeQuery = "orphaned function with no callers that is a bug risk"
	rerankTopN          = 20
	// rerankPreFilterN is the max docs sent to reranker; ~4s for 20 docs on ARM.
	rerankPreFilterN = 20
)

var rerankTimeout = 35 * time.Second // set in init via parseTimeoutSecs
var rerankHTTPClient *http.Client

func init() {
	rerankTimeout = parseTimeoutSecs("GOCODE_RERANK_TIMEOUT_S", 35*time.Second)
	rerankHTTPClient = &http.Client{Timeout: rerankTimeout}
}

// rerankRequest is the Cohere-compatible rerank API request.
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

// rerankResponse is the rerank API response.
type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

// RerankDeadCode reranks dead_code Cypher rows by likelihood of being
// actual dead code (not entrypoints or test utilities).
//
// Pre-filters to top rerankPreFilterN rows by complexity before calling the
// reranker — gte-multi-rerank takes ~0.2s/doc on ARM, so 20 docs ≈ 4s total.
//
// Returns the top rerankTopN rows sorted by relevance_score DESC.
// Falls back to original rows (capped at rerankTopN) on any error (non-fatal).
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

	// Build document strings for the reranker.
	docs := make([]string, len(candidates))
	candidateRows := make([][]string, len(candidates))
	for i, c := range candidates {
		docs[i] = formatDeadCodeDoc(c.row[0])
		candidateRows[i] = c.row
	}

	// Use a background context so the reranker call is not bound by the
	// MCP request context (which may be near its 60s deadline already).
	rerankCtx, cancel := context.WithTimeout(context.Background(), rerankTimeout)
	defer cancel()

	// Call reranker.
	ranked, err := callReranker(rerankCtx, rerankDeadCodeQuery, docs)
	if err != nil {
		slog.Warn("codegraph: dead_code rerank failed, using original order",
			slog.Any("error", err))
		if len(candidateRows) > rerankTopN {
			return candidateRows[:rerankTopN]
		}
		return candidateRows
	}

	// Sort by score DESC and take top N.
	sort.Slice(ranked.Results, func(i, j int) bool {
		return ranked.Results[i].RelevanceScore > ranked.Results[j].RelevanceScore
	})

	limit = min(rerankTopN, len(ranked.Results))

	reranked := make([][]string, 0, limit)
	for _, r := range ranked.Results[:limit] {
		if r.Index < len(candidateRows) {
			reranked = append(reranked, candidateRows[r.Index])
		}
	}

	topScore := 0.0
	if len(ranked.Results) > 0 {
		topScore = ranked.Results[0].RelevanceScore
	}

	slog.Info("codegraph: dead_code reranked",
		slog.Int("input", len(rows)),
		slog.Int("output", len(reranked)),
		slog.Float64("top_score", topScore))

	return reranked
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

// callReranker makes a POST request to the embed-server rerank endpoint.
func callReranker(ctx context.Context, query string, documents []string) (*rerankResponse, error) {
	body, err := json.Marshal(rerankRequest{
		Model:     rerankModel,
		Query:     query,
		Documents: documents,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rerankURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rerankHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result rerankResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("empty results")
	}
	return &result, nil
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

// callRerankerWithClient is like callReranker but uses a caller-supplied
// http.Client — for build-time scoring that needs longer timeout than rerankHTTPClient.
func callRerankerWithClient(ctx context.Context, client *http.Client, query string, documents []string) (*rerankResponse, error) {
	body, err := json.Marshal(rerankRequest{Model: rerankModel, Query: query, Documents: documents})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rerankURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var result rerankResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("empty results")
	}
	return &result, nil
}
