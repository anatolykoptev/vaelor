// Package main — eval harness for go-code retrieval quality.
//
// This file: HTTP client that drives go-code's REST bridge
// (POST /api/tools/semantic_search) and parses the XML response into a flat
// list of (file, symbol) pairs ranked top-1 first.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultHTTPTimeout is generous — semantic_search can trigger background
	// indexing on first hit and reply with a status placeholder; the harness
	// then retries on the next pass.
	defaultHTTPTimeout = 60 * time.Second

	// restToolPath is the REST bridge tool-call endpoint for semantic_search.
	restToolPath = "/api/tools/semantic_search"

	// restRepoAnalyzePath is the REST bridge tool-call endpoint for repo_analyze.
	restRepoAnalyzePath = "/api/tools/repo_analyze"
)

// ErrTransient signals a transient tool signal (soft-deadline timeout text or
// an indexing/pending/queued <status> envelope) that the caller should retry
// rather than record as a permanent hard failure. The harness retry loop
// (runWithRetry) checks errors.Is(err, ErrTransient{}) to decide whether to
// back off and retry or fail the query immediately.
type ErrTransient struct {
	Reason string
}

func (e ErrTransient) Error() string {
	return "transient: " + e.Reason
}

// Is makes errors.Is(err, ErrTransient{}) match any ErrTransient value,
// including when wrapped by fmt.Errorf("...: %w", ...).
func (e ErrTransient) Is(target error) bool {
	_, ok := target.(ErrTransient)
	return ok
}

// transientStatuses are <status> envelope values that mean "the repo is not
// ready yet — retry later". Case-insensitive match.
var transientStatuses = map[string]bool{
	"indexing": true,
	"pending":  true,
	"queued":   true,
}

// statusProbe is a minimal XML decoder used only to extract the <status>
// element from a payload, without committing to the full response schema.
type statusProbe struct {
	Status string `xml:"status"`
}

// classifyPayload inspects a raw tool payload and returns ErrTransient when it
// is a transient tool signal (not a real result or a real error). Used by BOTH
// parseSemanticXML and parseRepoAnalyzeXML so both modes share the same
// transient classification.
//
// Transient signals:
//   - plain-text soft-deadline timeout (contains "timed out during query
//     embedding" or starts with "semantic_search: timed out");
//   - any non-empty, non-XML payload (defensive: a non-XML tool message is
//     a transient signal, not a parse crash);
//   - a <status> envelope whose status is indexing/pending/queued.
//
// Returns nil for: empty string (caller returns nil,nil), and genuine XML
// payloads (real <results> or a ready-empty envelope) — the caller proceeds
// with normal parsing. Malformed XML that starts with '<' and isn't a
// status/timeout returns nil so the real parser produces the real parse error
// (non-transient).
func classifyPayload(payload string) error {
	if payload == "" {
		return nil
	}
	trimmed := strings.TrimSpace(payload)

	// Soft-deadline timeout text (plain text, not XML).
	if strings.Contains(payload, "timed out during query embedding") ||
		strings.HasPrefix(trimmed, "semantic_search: timed out") {
		return ErrTransient{Reason: "soft deadline: " + firstLine(payload)}
	}

	// Defensive: any non-XML tool message is transient, not a parse crash.
	if !strings.HasPrefix(trimmed, "<") {
		return ErrTransient{Reason: "non-XML tool message: " + firstLine(payload)}
	}

	// XML payload — probe for a <status> envelope.
	var sp statusProbe
	if err := xml.Unmarshal([]byte(payload), &sp); err == nil && sp.Status != "" {
		if transientStatuses[strings.ToLower(sp.Status)] {
			return ErrTransient{Reason: "repo status: " + sp.Status}
		}
	}

	// Genuine XML (real results or a ready-empty envelope) — parse normally.
	return nil
}

// firstLine returns the first line of s (without the trailing newline), or s
// itself if it has no newline. Used to keep ErrTransient reasons concise.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// MCPClient calls the go-code REST bridge for semantic_search.
type MCPClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMCPClient returns a client targeted at baseURL (e.g. http://127.0.0.1:8897).
func NewMCPClient(baseURL string) *MCPClient {
	return &MCPClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// SearchHit is one ranked result. Position is 1-based.
type SearchHit struct {
	Position int
	File     string
	Symbol   string
	Source   string
	Distance float64
}

// restCallToolResp matches rest.go's toolCallResponse struct.
type restCallToolResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"is_error"`
}

// Search calls semantic_search via the REST bridge and parses the response.
//
// repo / query / topK map directly to the tool's input schema. When language
// is non-empty it is passed as the `language` filter to semantic_search. When
// the server replies with a <status> envelope (e.g. "indexing"), Search
// returns an empty slice and a nil error — the caller decides whether to retry.
func (c *MCPClient) Search(ctx context.Context, repo, query, language string, topK int) ([]SearchHit, error) {
	args := map[string]any{
		"repo":  repo,
		"query": query,
	}
	if topK > 0 {
		args["top_k"] = topK
	}
	if language != "" {
		args["language"] = language
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	url := c.BaseURL + restToolPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var parsed restCallToolResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.IsError {
		return nil, fmt.Errorf("tool returned error: %s", joinContent(parsed.Content))
	}
	return parseSemanticXML(joinContent(parsed.Content))
}

// joinContent concatenates all text content blocks (semantic_search emits one).
func joinContent(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var sb strings.Builder
	for _, c := range content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// xmlResponse mirrors formatSemanticResults's output. Only the fields the
// harness uses are kept; <status>/<message> envelopes parse to zero results.
type xmlResponse struct {
	XMLName xml.Name `xml:"response"`
	Results struct {
		Result []xmlResult `xml:"result"`
	} `xml:"results"`
}

type xmlResult struct {
	Rank     int     `xml:"rank,attr"`
	Distance float64 `xml:"distance,attr"`
	Source   string  `xml:"source,attr"`
	File     string  `xml:"file"`
	Symbol   xmlSym  `xml:"symbol"`
}

type xmlSym struct {
	Kind string `xml:"kind,attr"`
	Name string `xml:",chardata"`
}

// parseSemanticXML extracts ranked hits from semantic_search's XML response.
//
// Transient tool signals (soft-deadline timeout text, indexing/pending/queued
// <status> envelope) are classified by classifyPayload and returned as
// ErrTransient so the caller can retry. A genuine <results> payload parses to
// hits. A definitively-ready envelope with zero results (status ok/none, empty
// <results>) → empty slice + nil (a REAL zero, not transient). Empty string →
// nil,nil. Malformed XML that starts with '<' and isn't a status/timeout → the
// real parse xml: error (non-transient). Hits are sorted by their declared
// rank attribute to be defensive against any future server-side reordering
// bugs.
func parseSemanticXML(payload string) ([]SearchHit, error) {
	if payload == "" {
		return nil, nil
	}
	if err := classifyPayload(payload); err != nil {
		return nil, err
	}
	var parsed xmlResponse
	if err := xml.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	hits := make([]SearchHit, 0, len(parsed.Results.Result))
	for _, r := range parsed.Results.Result {
		hits = append(hits, SearchHit{
			Position: r.Rank,
			File:     r.File,
			Symbol:   strings.TrimSpace(r.Symbol.Name),
			Source:   r.Source,
			Distance: r.Distance,
		})
	}
	return hits, nil
}

// RepoAnalyze calls the repo_analyze tool (deep mode) via the REST bridge and
// returns the ranked file paths in the tool's relevance order (BM25F+PageRank
// fusion, controlled server-side by ANALYZE_RANK_FUSION_MODE). The XML
// response's <files><file path="..."/></files> section is emitted in
// relevance-ranked order by the tool; this method preserves that order.
//
// When language is non-empty it is passed as the `language` filter; when
// empty no filter is sent. depth=deep is requested so the full ranked file
// list is emitted (overview depth omits the <files> section entirely).
//
// Note: when the target server has OUTPUT_DIR set and the deep-mode XML
// exceeds the inline threshold (50k chars), the tool returns a file-summary
// text instead of the XML envelope. That text is not XML, so parsing returns
// an error and the query becomes an error result — dropped from the paired
// gate and visible via PairedQueries (a shrinking denominator, or
// INSUFFICIENT_DATA below 2 pairs), never a silent score-0. For faithful
// benchmarking, run the target server with OUTPUT_DIR unset.
func (c *MCPClient) RepoAnalyze(ctx context.Context, repo, query, language string) ([]string, error) {
	args := map[string]any{
		"repo":  repo,
		"query": query,
		"depth": "deep",
	}
	if language != "" {
		args["language"] = language
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	url := c.BaseURL + restRepoAnalyzePath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var parsed restCallToolResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.IsError {
		return nil, fmt.Errorf("tool returned error: %s", joinContent(parsed.Content))
	}
	return parseRepoAnalyzeXML(joinContent(parsed.Content))
}

// repoAnalyzeXML mirrors the repo_analyze tool's XML envelope. Only the
// ranked <files> section is needed for file-level relevance scoring; all
// other sections (repo, packages, imports, tree, symbols, quality, etc.)
// are ignored by the decoder.
type repoAnalyzeXML struct {
	XMLName xml.Name `xml:"response"`
	Files   *struct {
		File []repoAnalyzeFile `xml:"file"`
	} `xml:"files,omitempty"`
}

type repoAnalyzeFile struct {
	Path string `xml:"path,attr"`
}

// parseRepoAnalyzeXML extracts the ranked file paths from a repo_analyze XML
// response, preserving document order (which is the tool's relevance order).
// Returns an empty slice when the response has no <files> section (e.g. an
// overview-depth response, or a file-summary text returned in place of XML).
// Transient tool signals (timeout text, indexing status) are classified by
// classifyPayload and returned as ErrTransient for retry by the caller.
func parseRepoAnalyzeXML(payload string) ([]string, error) {
	if payload == "" {
		return nil, nil
	}
	if err := classifyPayload(payload); err != nil {
		return nil, err
	}
	var parsed repoAnalyzeXML
	if err := xml.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("parse repo_analyze xml: %w", err)
	}
	if parsed.Files == nil {
		return nil, nil
	}
	out := make([]string, 0, len(parsed.Files.File))
	for _, f := range parsed.Files.File {
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}
	return out, nil
}
