package dozorclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/httputil"
)

// LogLine is a single parsed log entry from the dozor /api/logs response.
type LogLine struct {
	Ts    string `json:"ts"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Raw   string `json:"raw"`
}

// LogsResponse is the full response body from dozor /api/logs.
type LogsResponse struct {
	Service     string    `json:"service"`
	ContainerID string    `json:"container_id"`
	Lines       []LogLine `json:"lines"`
	Truncated   bool      `json:"truncated"`
}

// Client is an HTTP client for the dozor sidecar API.
type Client struct {
	http *httputil.Client
}

// NewClient creates a new dozor Client. token may be empty (no auth header sent).
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	opts := []httputil.Option{httputil.WithTimeout(timeout)}
	if token != "" {
		opts = append(opts, httputil.WithHeader("Authorization", "Bearer "+token))
	}
	return &Client{
		http: httputil.New(baseURL, opts...),
	}
}

// GetLogs fetches log lines from dozor for the given service and time window.
// since and until are optional (zero value means omit). grep is optional (empty
// means server applies its default filter: panic|fatal|error). limit <= 0 means
// use the server default.
func (c *Client) GetLogs(ctx context.Context, service string, since, until time.Time, grep string, limit int) (*LogsResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("dozor client nil")
	}
	q := url.Values{}
	q.Set("service", service)
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	if !until.IsZero() {
		q.Set("until", strconv.FormatInt(until.Unix(), 10))
	}
	if grep != "" {
		q.Set("grep", grep)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	var out LogsResponse
	if err := c.http.GetJSON(ctx, "/api/logs?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
