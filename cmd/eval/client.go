// Package main — eval harness for go-code retrieval quality.
//
// This file: HTTP client that drives go-code's REST bridge
// (POST /api/tools/semantic_search) and parses the XML response into a flat
// list of (file, symbol) pairs ranked top-1 first.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultHTTPTimeout is generous — semantic_search can trigger background
	// indexing on first hit and reply with a status placeholder; the harness
	// then retries on the next pass.
	defaultHTTPTimeout = 60 * time.Second

	// restToolPath is the REST bridge tool-call endpoint.
	restToolPath = "/api/tools/semantic_search"
)

// MCPClient calls the go-code REST bridge for semantic_search.
type MCPClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMCPClient returns a client targeted at baseURL (e.g. http://127.0.0.1:8897).
func NewMCPClient(baseURL string) *MCPClient {
	return &MCPClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// SearchHit is one ranked result. Position is 1-based.
type SearchHit struct {
	Position int
	File     string
	Symbol   string
	Source   string
	Distance float64
}

// restCallToolResp matches rest.go's toolCallResponse struct.
type restCallToolResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"is_error"`
}

// Search calls semantic_search via the REST bridge and parses the response.
//
// repo / query / topK map directly to the tool's input schema. When language
// is non-empty it is passed as the `language` filter to semantic_search. When
// the server replies with a <status> envelope (e.g. "indexing"), Search
// returns an empty slice and a nil error — the caller decides whether to retry.
func (c *MCPClient) Search(ctx context.Context, repo, query, language string, topK int) ([]SearchHit, error) {
	args := map[string]any{
		"repo":  repo,
		"query": query,
	}
	if topK > 0 {
		args["top_k"] = topK
	}
	if language != "" {
		args["language"] = language
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	url := c.BaseURL + restToolPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var parsed restCallToolResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.IsError {
		return nil, fmt.Errorf("tool returned error: %s", joinContent(parsed.Content))
	}
	return parseSemanticXML(joinContent(parsed.Content))
}

// joinContent concatenates all text content blocks (semantic_search emits one).
func joinContent(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var sb strings.Builder
	for _, c := range content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// xmlResponse mirrors formatSemanticResults's output. Only the fields the
// harness uses are kept; <status>/<message> envelopes parse to zero results.
type xmlResponse struct {
	XMLName xml.Name `xml:"response"`
	Results struct {
		Result []xmlResult `xml:"result"`
	} `xml:"results"`
}

type xmlResult struct {
	Rank     int     `xml:"rank,attr"`
	Distance float64 `xml:"distance,attr"`
	Source   string  `xml:"source,attr"`
	File     string  `xml:"file"`
	Symbol   xmlSym  `xml:"symbol"`
}

type xmlSym struct {
	Kind string `xml:"kind,attr"`
	Name string `xml:",chardata"`
}

// parseSemanticXML extracts ranked hits from semantic_search's XML response.
//
// When the response is a <status> envelope (no results), returns an empty slice.
// Hits are sorted by their declared rank attribute to be defensive against any
// future server-side reordering bugs.
func parseSemanticXML(payload string) ([]SearchHit, error) {
	if payload == "" {
		return nil, nil
	}
	var parsed xmlResponse
	if err := xml.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	hits := make([]SearchHit, 0, len(parsed.Results.Result))
	for _, r := range parsed.Results.Result {
		hits = append(hits, SearchHit{
			Position: r.Rank,
			File:     r.File,
			Symbol:   strings.TrimSpace(r.Symbol.Name),
			Source:   r.Source,
			Distance: r.Distance,
		})
	}
	return hits, nil
}
