// Package embeddings provides a client for OpenAI-compatible embedding APIs.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	maxBatch       = 8
	maxConcurrent  = 2
	requestTimeout = 120 * time.Second
	retryBackoff   = time.Second
)

// Client calls an OpenAI-compatible embeddings API.
type Client struct {
	url, model string
	http       *http.Client
}
type embeddingReq struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}
type embeddingResp struct {
	Data []struct{ Embedding []float32 `json:"embedding"` } `json:"data"`
}

// NewClient creates an embeddings client.
func NewClient(baseURL, model string) *Client {
	return &Client{url: baseURL + "/v1/embeddings", model: model, http: &http.Client{Timeout: requestTimeout}}
}

// Embed returns embeddings for texts with "passage: " prefix. Batches automatically.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = "passage: " + t
	}
	return c.embedBatched(ctx, prefixed)
}

// EmbedQuery embeds a single search query with "query: " prefix.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	res, err := c.embedBatched(ctx, []string{"query: " + query})
	if err != nil {
		return nil, err
	}
	return res[0], nil
}
func (c *Client) embedBatched(ctx context.Context, texts []string) ([][]float32, error) {
	n := (len(texts) + maxBatch - 1) / maxBatch
	results, errs := make([][]float32, len(texts)), make([]error, n)
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i := range n {
		s, e := i*maxBatch, min(i*maxBatch+maxBatch, len(texts))
		wg.Add(1)
		go func(idx, s, e int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if embs, err := c.doRequest(ctx, texts[s:e]); err != nil {
				errs[idx] = fmt.Errorf("batch %d: %w", idx, err)
			} else {
				copy(results[s:e], embs)
			}
		}(i, s, e)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}
func (c *Client) doRequest(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(embeddingReq{Input: texts, Model: c.model})
	for attempt := range 2 {
		if attempt > 0 { time.Sleep(retryBackoff) }
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 && attempt == 0 { continue }
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
		}
		var parsed embeddingResp
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		out := make([][]float32, len(parsed.Data))
		for i := range parsed.Data {
			out[i] = parsed.Data[i].Embedding
		}
		return out, nil
	}
	return nil, fmt.Errorf("unreachable")
}
