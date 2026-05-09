// Package httputil provides a thin JSON HTTP client shared by promclient,
// jaegerclient, and dozorclient. Each of those packages previously rolled its
// own http.Client.Do + json.Decode + status-check + error-wrap; this package
// consolidates the pattern.
package httputil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin wrapper around http.Client for JSON GET/POST.
type Client struct {
	base    string // base URL, no trailing slash
	http    *http.Client
	headers map[string]string
}

// Option configures a Client.
type Option func(*Client)

// WithHeader returns an Option that adds a fixed header to every request.
func WithHeader(k, v string) Option {
	return func(c *Client) { c.headers[k] = v }
}

// WithTimeout returns an Option that sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// New creates a Client targeting baseURL. Trailing slashes are stripped.
// Default timeout is 30 s if not overridden via WithTimeout.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		base:    strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
		headers: map[string]string{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewWithHTTPClient creates a Client that reuses an existing *http.Client.
// This preserves connection pooling when the caller already manages an
// http.Client instance (e.g. promclient, jaegerclient, dozorclient).
func NewWithHTTPClient(baseURL string, hc *http.Client, opts ...Option) *Client {
	c := &Client{
		base:    strings.TrimRight(baseURL, "/"),
		http:    hc,
		headers: map[string]string{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// GetJSON issues GET <base><path> and decodes a successful JSON response into
// dst (may be nil to discard body). Non-2xx status returns a wrapped error
// including the status code and up to 256 bytes of body for diagnostics.
func (c *Client) GetJSON(ctx context.Context, path string, dst any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, dst)
}

// PostJSON issues POST <base><path> with body marshalled as JSON and decodes
// the response into dst (may be nil).
func (c *Client) PostJSON(ctx context.Context, path string, body, dst any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, dst)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, dst any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		rdr = strings.NewReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(excerpt)))
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
