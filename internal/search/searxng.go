package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const defaultHTTPTimeout = 15 * time.Second

// SearXNGClient queries a SearXNG instance.
type SearXNGClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSearXNGClient creates a SearXNGClient targeting the given base URL.
func NewSearXNGClient(baseURL string) *SearXNGClient {
	return &SearXNGClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// searxngResponse is the JSON envelope returned by the SearXNG /search endpoint.
type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// Search queries SearXNG and returns results.
func (c *SearXNGClient) Search(ctx context.Context, query string, opts SearchOpts) ([]Result, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")

	if opts.Language != "" && opts.Language != "all" {
		params.Set("language", opts.Language)
	}
	if opts.TimeRange != "" {
		params.Set("time_range", opts.TimeRange)
	}
	if opts.Engines != "" {
		params.Set("engines", opts.Engines)
	}

	reqURL := c.baseURL + "/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("search: build request: %w", err)
	}
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL comes from trusted server config, not user input
	if err != nil {
		return nil, fmt.Errorf("search: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search: unexpected status %d", resp.StatusCode)
	}

	var body searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("search: decode response: %w", err)
	}

	results := make([]Result, len(body.Results))
	for i, r := range body.Results {
		results[i] = Result(r)
	}
	return results, nil
}

// FilterByScore removes results below minScore, keeping at least minKeep.
// If the filtered count is less than minKeep, the first minKeep results from
// the original slice are returned. If the original has fewer than minKeep
// results, all original results are returned.
func FilterByScore(results []Result, minScore float64, minKeep int) []Result {
	filtered := make([]Result, 0, len(results))
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) >= minKeep {
		return filtered
	}

	// Fall back to the first minKeep entries from the original slice.
	if len(results) <= minKeep {
		return results
	}
	return results[:minKeep]
}

// DedupByDomain limits results to maxPerDomain per domain.
// Results whose URL cannot be parsed are always included.
func DedupByDomain(results []Result, maxPerDomain int) []Result {
	counts := make(map[string]int, len(results))
	out := make([]Result, 0, len(results))

	for _, r := range results {
		u, err := url.Parse(r.URL)
		if err != nil || u.Host == "" {
			// Unparseable URL — include unconditionally.
			out = append(out, r)
			continue
		}
		host := u.Host
		if counts[host] < maxPerDomain {
			counts[host]++
			out = append(out, r)
		}
	}
	return out
}
