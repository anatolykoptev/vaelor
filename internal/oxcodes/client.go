// Package oxcodes provides an HTTP client for the ox-codes search service.
package oxcodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const httpTimeout = 30 * time.Second

// Client calls ox-codes HTTP API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates an ox-codes client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// SearchInput mirrors ox-codes /search request.
type SearchInput struct {
	Root          string `json:"root"`
	Pattern       string `json:"pattern"`
	IsRegex       bool   `json:"is_regex"`
	FileGlob      string `json:"file_glob,omitempty"`
	ExcludeGlob   string `json:"exclude_glob,omitempty"`
	ContextLines  int    `json:"context_lines"`
	MaxResults    int    `json:"max_results"`
	CaseSensitive bool   `json:"case_sensitive"`
	Language      string `json:"language,omitempty"`
	Expand        string `json:"expand,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
}

// ScopedSearchInput mirrors ox-codes /search/scoped request.
type ScopedSearchInput struct {
	Root          string `json:"root"`
	Pattern       string `json:"pattern"`
	Scope         string `json:"scope"`
	Language      string `json:"language"`
	IsRegex       bool   `json:"is_regex"`
	MaxResults    int    `json:"max_results"`
	CaseSensitive bool   `json:"case_sensitive"`
	ExcludeGlob   string `json:"exclude_glob,omitempty"`
	Expand        string `json:"expand,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
}

// StructuralSearchInput mirrors ox-codes /search/structural request.
type StructuralSearchInput struct {
	Root        string `json:"root"`
	Pattern     string `json:"pattern"`
	Language    string `json:"language"`
	MaxResults  int    `json:"max_results"`
	ExcludeGlob string `json:"exclude_glob,omitempty"`
	Expand      string `json:"expand,omitempty"`
	MaxTokens   int    `json:"max_tokens,omitempty"`
}

// RewriteInput mirrors ox-codes /rewrite request.
type RewriteInput struct {
	Root        string `json:"root"`
	Pattern     string `json:"pattern"`
	Rewrite     string `json:"rewrite"`
	Language    string `json:"language"`
	MaxResults  int    `json:"max_results"`
	FileGlob    string `json:"file_glob,omitempty"`
	ExcludeGlob string `json:"exclude_glob,omitempty"`
}

// SearchResponse mirrors ox-codes response.
type SearchResponse struct {
	Matches      []SearchMatch `json:"matches"`
	TotalMatches int           `json:"total_matches"`
	Truncated    bool          `json:"truncated"`
	DurationMS   int64         `json:"duration_ms"`
}

// ExpandedBlock holds the expanded AST context for a match.
type ExpandedBlock struct {
	SymbolName string `json:"symbol_name"`
	SymbolKind string `json:"symbol_kind"`
	LineStart  int    `json:"line_start"`
	LineEnd    int    `json:"line_end"`
	Body       string `json:"body"`
}

// SearchMatch mirrors ox-codes match.
type SearchMatch struct {
	File     string         `json:"file"`
	Line     int            `json:"line"`
	Text     string         `json:"text"`
	Context  []string       `json:"context,omitempty"`
	Expanded *ExpandedBlock `json:"expanded,omitempty"`
}

// RewriteResponse mirrors ox-codes /rewrite response.
type RewriteResponse struct {
	Files        []RewriteFileResult `json:"files"`
	TotalMatches int                 `json:"total_matches"`
	TotalFiles   int                 `json:"total_files"`
	DurationMS   int64               `json:"duration_ms"`
}

// RewriteFileResult holds the per-file rewrite result.
type RewriteFileResult struct {
	File    string `json:"file"`
	Matches int    `json:"matches"`
	Diff    string `json:"diff"`
}

// Search calls POST /search.
func (c *Client) Search(ctx context.Context, input SearchInput) (*SearchResponse, error) {
	return c.post(ctx, "/search", input)
}

// SearchScoped calls POST /search/scoped.
func (c *Client) SearchScoped(ctx context.Context, input ScopedSearchInput) (*SearchResponse, error) {
	return c.post(ctx, "/search/scoped", input)
}

// SearchStructural calls POST /search/structural.
func (c *Client) SearchStructural(ctx context.Context, input StructuralSearchInput) (*SearchResponse, error) {
	return c.post(ctx, "/search/structural", input)
}

// Rewrite calls POST /rewrite.
func (c *Client) Rewrite(ctx context.Context, input RewriteInput) (*RewriteResponse, error) {
	var result RewriteResponse
	if err := c.doPost(ctx, "/rewrite", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) post(ctx context.Context, path string, body any) (*SearchResponse, error) {
	var result SearchResponse
	if err := c.doPost(ctx, path, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) doPost(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("oxcodes: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("oxcodes: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oxcodes: request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oxcodes: status %d: %s", resp.StatusCode, string(errBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
