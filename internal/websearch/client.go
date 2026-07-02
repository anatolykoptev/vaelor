// Package websearch provides an HTTP client for go-search MCP server.
// Used by repo_search to discover repositories via web search.
package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/httputil"
)

const httpTimeout = 15 * time.Second

// Client calls go-search MCP server via HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a go-search client targeting the given MCP base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// Result is a single web search result.
type Result struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// searchOutput matches go-search's SmartSearchOutput (only fields we need).
type searchOutput struct {
	Query   string       `json:"query"`
	Sources []sourceItem `json:"sources,omitempty"`
}

type sourceItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// mcpRequest is the JSON-RPC request for MCP tool calls.
type mcpRequest struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int       `json:"id"`
	Method  string    `json:"method"`
	Params  mcpParams `json:"params"`
}

type mcpParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// mcpResponse is the JSON-RPC response from MCP.
type mcpResponse struct {
	Result struct {
		Content []mcpContent `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Search calls go-search smart_search with depth=fast and returns raw results.
// The JSON-RPC envelope is built/parsed here; the HTTP transport (marshal,
// POST, decode) delegates to httputil.Client to avoid duplicating that
// plumbing (mirrors jaegerclient/promclient/dozorclient).
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	args, err := json.Marshal(map[string]string{
		"query": query,
		"depth": "fast",
	})
	if err != nil {
		return nil, fmt.Errorf("websearch: marshal args: %w", err)
	}

	reqBody := mcpRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  mcpParams{Name: "smart_search", Arguments: args},
	}

	var mcpResp mcpResponse
	hc := httputil.NewWithHTTPClient(c.baseURL, c.httpClient)
	if err := hc.PostJSON(ctx, "", reqBody, &mcpResp); err != nil {
		return nil, fmt.Errorf("websearch: %w", err)
	}
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("websearch: mcp error: %s", mcpResp.Error.Message)
	}

	return extractResults(mcpResp)
}

// extractResults parses MCP tool response content into Results.
func extractResults(mcpResp mcpResponse) ([]Result, error) {
	for _, c := range mcpResp.Result.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}
		var out searchOutput
		if err := json.Unmarshal([]byte(c.Text), &out); err != nil {
			continue
		}
		results := make([]Result, 0, len(out.Sources))
		for _, s := range out.Sources {
			results = append(results, Result{
				Title: s.Title,
				URL:   s.URL,
			})
		}
		return results, nil
	}
	return nil, nil
}
