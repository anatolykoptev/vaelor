package jaegerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
// response body into dest. Non-2xx status codes are returned as errors
// including up to 256 bytes of the response body for debug visibility.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyPreview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("jaeger HTTP %d: %s", resp.StatusCode, string(bodyPreview))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
