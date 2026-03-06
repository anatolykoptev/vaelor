package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func newMockServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req embeddingReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := embeddingResp{Data: make([]struct {
			Embedding []float32 `json:"embedding"`
		}, len(req.Input))}
		for i := range req.Input {
			resp.Data[i].Embedding = make([]float32, 4) // short vector for tests
			resp.Data[i].Embedding[0] = float32(i)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEmbed_Empty(t *testing.T) {
	c := NewClient("http://unused", "test-model")
	result, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestEmbed_SingleText(t *testing.T) {
	var calls atomic.Int32
	srv := newMockServer(t, &calls)
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	result, err := c.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(result))
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 API call, got %d", calls.Load())
	}
}

func TestEmbed_BatchSplitting(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		wantCalls int32
	}{
		{"33 texts = 2 batches", 33, 2},
		{"64 texts = 2 batches", 64, 2},
		{"32 texts = 1 batch", 32, 1},
		{"65 texts = 3 batches", 65, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := newMockServer(t, &calls)
			defer srv.Close()

			texts := make([]string, tt.count)
			for i := range texts {
				texts[i] = "text"
			}
			c := NewClient(srv.URL, "test-model")
			result, err := c.Embed(context.Background(), texts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.count {
				t.Fatalf("expected %d embeddings, got %d", tt.count, len(result))
			}
			if calls.Load() != tt.wantCalls {
				t.Fatalf("expected %d API calls, got %d", tt.wantCalls, calls.Load())
			}
		})
	}
}

func TestPrefix_Passage(t *testing.T) {
	var lastInput []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingReq
		json.NewDecoder(r.Body).Decode(&req)
		lastInput = req.Input
		resp := embeddingResp{Data: make([]struct {
			Embedding []float32 `json:"embedding"`
		}, len(req.Input))}
		for i := range resp.Data {
			resp.Data[i].Embedding = []float32{0}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	c.Embed(context.Background(), []string{"hello", "world"})

	if lastInput[0] != "passage: hello" || lastInput[1] != "passage: world" {
		t.Fatalf("expected passage prefix, got %v", lastInput)
	}
}

func TestEmbedQuery_Prefix(t *testing.T) {
	var lastInput []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingReq
		json.NewDecoder(r.Body).Decode(&req)
		lastInput = req.Input
		resp := embeddingResp{Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{{Embedding: []float32{1, 2, 3}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	vec, err := c.EmbedQuery(context.Background(), "search term")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastInput[0] != "query: search term" {
		t.Fatalf("expected query prefix, got %s", lastInput[0])
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3-dim vector, got %d", len(vec))
	}
}
