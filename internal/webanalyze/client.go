package webanalyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const clientTimeout = 30 * time.Second

// Client calls ox-browser HTTP API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates an ox-browser client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: clientTimeout},
	}
}

// Technology is a detected web technology.
type Technology struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence int    `json:"confidence"`
}

// Meta holds page metadata.
type Meta struct {
	Generator string `json:"generator"`
	Server    string `json:"server"`
	PoweredBy string `json:"powered_by"`
	Title     string `json:"title"`
}

// Assets holds discovered script and stylesheet URLs.
type Assets struct {
	Scripts     []string `json:"scripts"`
	Stylesheets []string `json:"stylesheets"`
}

// AnalyzeResponse is the response from ox-browser /analyze.
type AnalyzeResponse struct {
	URL          string       `json:"url"`
	Status       int          `json:"status"`
	Technologies []Technology `json:"technologies"`
	Meta         Meta         `json:"meta"`
	Assets       Assets       `json:"assets"`
	Method       string       `json:"method"`
	CFDetected   bool         `json:"cf_detected"`
	ElapsedMs    int          `json:"elapsed_ms"`
	Error        string       `json:"error,omitempty"`
}

// FetchResponse is the response from ox-browser /fetch.
type FetchResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error,omitempty"`
}

// Analyze calls POST /analyze on ox-browser.
func (c *Client) Analyze(ctx context.Context, url string) (*AnalyzeResponse, error) {
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("analyze request: %w", err)
	}
	defer resp.Body.Close()

	var result AnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Fetch calls POST /fetch on ox-browser to download a single URL.
func (c *Client) Fetch(ctx context.Context, url string) (*FetchResponse, error) {
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/fetch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch request: %w", err)
	}
	defer resp.Body.Close()

	var result FetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
