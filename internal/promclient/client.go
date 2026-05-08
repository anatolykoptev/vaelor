package promclient

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

// Client is a minimal HTTP client for the Prometheus query API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Client. If timeout is 0 or negative, defaultTimeout (30s) is used.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// GetJSON performs a GET request to the given path (relative to baseURL),
// decodes the JSON response body into dest, and returns any error.
func (c *Client) GetJSON(ctx context.Context, path string, dest any) error {
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
		// Include first 256 bytes of body for debug visibility.
		bodyPreview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("prometheus HTTP %d: %s", resp.StatusCode, string(bodyPreview))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
