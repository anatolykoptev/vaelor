package promclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/httputil"
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
// Delegates to httputil.Client to avoid duplicating http+json plumbing.
func (c *Client) GetJSON(ctx context.Context, path string, dest any) error {
	return httputil.NewWithHTTPClient(c.baseURL, c.httpClient).GetJSON(ctx, path, dest)
}

// Alert represents a single Prometheus alerting rule result from /api/v1/alerts.
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"` // "firing"|"pending"|"inactive"
	ActiveAt    string            `json:"activeAt"`
	Value       string            `json:"value"`
}

// Alerts queries /api/v1/alerts and returns all alert instances regardless of state.
// Callers should filter by State == "firing" for actionable alerts.
func (c *Client) Alerts(ctx context.Context) ([]Alert, error) {
	type resp struct {
		Status string `json:"status"`
		Data   struct {
			Alerts []Alert `json:"alerts"`
		} `json:"data"`
	}
	var r resp
	if err := c.GetJSON(ctx, "/api/v1/alerts", &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("alerts status %q", r.Status)
	}
	return r.Data.Alerts, nil
}

// MetricNames fetches the list of all metric names from Prometheus
// (/api/v1/label/__name__/values). Returns empty slice on error.
// This is the single source of truth for metric name discovery — callers
// should fetch once and pass the result to discover* filter functions.
func (c *Client) MetricNames(ctx context.Context) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	if err := c.GetJSON(ctx, "/api/v1/label/__name__/values", &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("metric names status %q", r.Status)
	}
	return r.Data, nil
}
