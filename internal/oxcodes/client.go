// Package oxcodes provides an HTTP client for the ox-codes search service.
package oxcodes

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/httputil"
)

// httpTimeout is a generous ceiling: a cold ast-grep structural search on a
// large repo (go-code itself is ~3300 files) compiles the pattern + does a
// first full scan, which can exceed 10s on the initial query. The timeout is
// only a ceiling — warm/fast calls (regular search, dataflow) return well
// before it.
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
	Format        string `json:"format,omitempty"` // "markdown" wraps expanded bodies in ```lang fences
}

// ScopedSearchInput mirrors ox-codes /search/scoped request.
type ScopedSearchInput struct {
	Root          string `json:"root"`
	Pattern       string `json:"pattern"`
	Scope         string `json:"scope"`
	Language      string `json:"language,omitempty"` // omitempty: empty language omitted; ox-codes /search/scoped rejects ""
	IsRegex       bool   `json:"is_regex"`
	MaxResults    int    `json:"max_results"`
	CaseSensitive bool   `json:"case_sensitive"`
	FileGlob      string `json:"file_glob,omitempty"`
	ExcludeGlob   string `json:"exclude_glob,omitempty"`
	Expand        string `json:"expand,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
	Format        string `json:"format,omitempty"` // "markdown" wraps expanded bodies in ```lang fences
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
	Format      string `json:"format,omitempty"` // "markdown" wraps expanded bodies in ```lang fences
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
	Apply       bool   `json:"apply,omitempty"`
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

// doPost issues the JSON POST and decodes the response. Delegates to
// httputil.Client to avoid duplicating http+json plumbing (mirrors
// jaegerclient/promclient/dozorclient).
func (c *Client) doPost(ctx context.Context, path string, body any, result any) error {
	hc := httputil.NewWithHTTPClient(c.baseURL, c.httpClient)
	if err := hc.PostJSON(ctx, path, body, result); err != nil {
		return fmt.Errorf("oxcodes: %w", err)
	}
	return nil
}
