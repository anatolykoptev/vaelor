package webanalyze

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const clientTimeout = 30 * time.Second

// crawlTimeout is longer because crawls can take minutes for large sites.
const crawlTimeout = 10 * time.Minute

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
	Name       string   `json:"name"`
	Categories []string `json:"categories"`
	Confidence int      `json:"confidence"`
	Version    *string  `json:"version,omitempty"`
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
	URL           string              `json:"url"`
	Status        int                 `json:"status"`
	Technologies  []Technology        `json:"technologies"`
	Meta          Meta                `json:"meta"`
	Assets        Assets              `json:"assets"`
	SEO           SeoReport           `json:"seo"`
	Performance   PerformanceReport   `json:"performance"`
	Accessibility AccessibilityReport `json:"accessibility"`
	Content       ContentReport       `json:"content"`
	Media         MediaReport         `json:"media"`
	Fonts         FontsReport         `json:"fonts"`
	PWA           PwaReport           `json:"pwa"`
	API           ApiReport           `json:"api"`
	Method        string              `json:"method"`
	CFDetected    bool                `json:"cf_detected"`
	ElapsedMs     int                 `json:"elapsed_ms"`
	Error         string              `json:"error,omitempty"`
}

// FetchResponse is the response from ox-browser /fetch.
type FetchResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error,omitempty"`
}

// Analyze calls POST /analyze on ox-browser. Not migrated to httputil.Client:
// ox-browser returns HTTP 502 + a JSON body whose Error field carries the
// failure reason (e.g. target unreachable — see ox-browser
// crates/js/src/analyze.rs), and httputil discards non-2xx response bodies
// without decoding them. This client keeps its own decode-regardless-of-status
// transport so a 502 still yields a populated AnalyzeResponse.Error instead of
// a truncated opaque error (same carve-out as internal/fleet/upstream).
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

// Fetch calls POST /fetch on ox-browser to download a single URL. Same
// non-2xx-carries-a-decodable-error-body contract as Analyze, so it keeps
// the same hand-rolled decode-regardless-of-status transport.
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

// CrawlInput holds parameters for the crawl request.
type CrawlInput struct {
	URL             string `json:"url"`
	MaxDepth        int    `json:"max_depth"`
	MaxPages        int    `json:"max_pages"`
	Scope           string `json:"scope,omitempty"`
	IncludeMarkdown bool   `json:"include_markdown"`
}

// Crawl calls POST /crawl on ox-browser and consumes the SSE stream. Not
// migrated to httputil.Client: the response is a long-lived Server-Sent
// Events stream, not a single JSON body, which httputil.Client does not
// support.
func (c *Client) Crawl(ctx context.Context, input CrawlInput) (*CrawlResponse, error) {
	body, _ := json.Marshal(input)
	ctx, cancel := context.WithTimeout(ctx, crawlTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/crawl", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Use a client without the default Timeout — SSE streams are long-lived.
	// Context timeout handles cancellation; Transport is shared via DefaultTransport.
	sseClient := &http.Client{}
	resp, err := sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawl request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain body so the TCP connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("crawl: status %d", resp.StatusCode)
	}

	return parseSSECrawl(resp.Body)
}

// parseSSECrawl reads an SSE stream and collects pages + summary.
func parseSSECrawl(r io.Reader) (*CrawlResponse, error) {
	scanner := bufio.NewScanner(r)
	result := &CrawlResponse{}

	var eventType string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch eventType {
			case "page":
				var page CrawlPage
				if err := json.Unmarshal([]byte(data), &page); err == nil {
					result.Pages = append(result.Pages, page)
				}
			case "done":
				var summary CrawlSummary
				if err := json.Unmarshal([]byte(data), &summary); err == nil {
					result.Summary = summary
				}
			}
			eventType = ""
			continue
		}
	}
	return result, scanner.Err()
}
