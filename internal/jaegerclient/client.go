package jaegerclient

import (
	"context"
	"time"

	"github.com/anatolykoptev/vaelor/internal/httputil"
)

const defaultTimeout = 30 * time.Second

// Client is a lightweight HTTP client for the Jaeger query API.
type Client struct {
	httpClient *httputil.Client
}

// NewClient creates a new Client targeting baseURL. If timeout is zero or
// negative the default of 30 s is used. Trailing slashes are stripped from
// baseURL so callers need not worry about double-slash paths.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		httpClient: httputil.New(baseURL, httputil.WithTimeout(timeout)),
	}
}

// getJSON performs a GET to path (relative to baseURL) and decodes the JSON
// response body into dest. Delegates to httputil.Client to avoid duplicating
// http+json plumbing.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	return c.httpClient.GetJSON(ctx, path, dest)
}
