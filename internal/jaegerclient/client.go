package jaegerclient

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/httputil"
)

const defaultTimeout = 30 * time.Second

// Client is a lightweight HTTP client for the Jaeger query API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Client targeting baseURL. If timeout is zero or
// negative the default of 30 s is used. Trailing slashes are stripped from
// baseURL so callers need not worry about double-slash paths.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// getJSON performs a GET to path (relative to baseURL) and decodes the JSON
// response body into dest. Delegates to httputil.Client to avoid duplicating
// http+json plumbing.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	return httputil.NewWithHTTPClient(c.baseURL, c.httpClient).GetJSON(ctx, path, dest)
}
