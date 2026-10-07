package jeff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultTimeout bounds a call when the caller's context carries no earlier
// deadline. It is headroom, not a latency target: latency scales with the
// label count (see the package doc), so callers on hot paths should pass a
// ctx sized to their question shape.
const defaultTimeout = 10 * time.Second

// defaultModel is the Jev contract's conventional model name; the server
// resolves it through its configured alias map.
const defaultModel = "jev-latest"

// Client is a jeff System One client. Construct via NewClient.
type Client struct {
	url         string
	token       string
	model       string
	timeout     time.Duration
	http        *http.Client
	requireAuth bool
}

// Opt configures a Client.
type Opt func(*Client)

// WithToken sets the bearer token. Empty means "resolve from JEFF_TOKEN".
func WithToken(token string) Opt {
	return func(c *Client) { c.token = token }
}

// WithModel sets the model name sent in requests (default "jev-latest").
func WithModel(model string) Opt {
	return func(c *Client) { c.model = model }
}

// WithTimeout sets the per-call cap applied via http.Client.Timeout
// (default 10s). A stricter caller ctx still wins.
func WithTimeout(d time.Duration) Opt {
	return func(c *Client) { c.timeout = d }
}

// WithHTTPClient replaces the HTTP client entirely (custom transports,
// instrumentation). WithTimeout is ignored when this is set — the
// provided client's own timeout applies.
func WithHTTPClient(h *http.Client) Opt {
	return func(c *Client) { c.http = h }
}

// WithRequireAuth fails NewClient with ErrNoToken when no bearer token
// resolves. Without it a missing token logs once and proceeds — for
// self-hosted deployments without auth.
func WithRequireAuth() Opt {
	return func(c *Client) { c.requireAuth = true }
}

// NewClient builds a jeff client for baseURL (e.g. "https://jeff.example.com").
func NewClient(baseURL string, opts ...Opt) (*Client, error) {
	c := &Client{
		url:     strings.TrimRight(baseURL, "/"),
		model:   defaultModel,
		timeout: defaultTimeout,
	}
	for _, o := range opts {
		o(c)
	}
	if c.token == "" {
		c.token = os.Getenv("JEFF_TOKEN")
	}
	if strings.TrimSpace(c.token) == "" {
		if c.requireAuth {
			return nil, ErrNoToken
		}
		slog.Info("jeff: no auth token configured — assuming self-hosted backend without auth")
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: c.timeout}
	}
	return c, nil
}

// Ask posts a System One request and returns the decoded response.
// The caller's ctx bounds the call; the client's timeout is a cap on top.
func (c *Client) Ask(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("jeff: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jeff: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		jeffRequestsTotal.WithLabelValues("transport_error").Inc()
		return nil, fmt.Errorf("jeff: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		jeffRequestsTotal.WithLabelValues("http_error").Inc()
		return nil, &StatusError{StatusCode: resp.StatusCode}
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		jeffRequestsTotal.WithLabelValues("decode_error").Inc()
		return nil, fmt.Errorf("jeff: decode response: %w", err)
	}
	jeffRequestsTotal.WithLabelValues("ok").Inc()
	return &result, nil
}

// AskNoul is the one-question convenience: posts a single noul question
// and returns its probability in 0..1. ErrNoAnswer wraps a response that
// carries no answer for it.
func (c *Client) AskNoul(ctx context.Context, state any, question string) (float64, error) {
	const qn = "q"
	resp, err := c.Ask(ctx, Request{
		State:     state,
		Questions: map[string]Question{qn: NoulQuestion(question)},
	})
	if err != nil {
		return 0, err
	}
	a, ok := resp.Answers[qn]
	if !ok {
		return 0, ErrNoAnswer
	}
	return a.Noul, nil
}
