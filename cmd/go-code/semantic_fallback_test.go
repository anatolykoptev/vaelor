package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-kit/embed"
)

// newSlowEmbedServer returns an httptest.Server that sleeps for delay then
// returns HTTP 504 (Gateway Timeout), modelling a dead upstream embed server.
// This ensures EmbedQuery returns an error — Store.Search is never reached.
func newSlowEmbedServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
		case <-r.Context().Done():
			// Client cancelled — do nothing.
		}
	}))
}

// TestSemanticSuggestFallbackEmbedTimeout verifies that semanticSuggest
// returns within the sub-context budget (semanticFallbackEmbedTimeout = 5s)
// even when the embed server sleeps for 10s.
//
// RED: without the 5s sub-context added in Fix #2, EmbedQuery inherits the
// parent context (30s) and semanticSuggest blocks the full 10s server delay.
//
// GREEN: with Fix #2, semanticSuggest caps EmbedQuery at 5s via
// context.WithTimeout(ctx, semanticFallbackEmbedTimeout), returning an empty
// string in ~5s regardless of the server delay.
//
// Anti-tautology: drives the *production* semanticSuggest function with a
// real HTTP test server sleeping 10s. The timing assertion distinguishes
// "timeout applied" (< 6s) from "timeout not applied" (≥ 10s).
func TestSemanticSuggestFallbackEmbedTimeout(t *testing.T) {
	srv := newSlowEmbedServer(t, 10*time.Second)
	defer srv.Close()

	// Build an embed.Client pointing at the slow test server.
	// 768-dim jina-code-v2, no retries, no circuit breaker.
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(768),
		embed.WithModel("jina-code-v2"),
	)
	if err != nil {
		t.Fatalf("embed.NewClient: %v", err)
	}

	// NewStore(nil): non-nil *Store but with a nil pool.
	// Safe in this test — Store.Search is never reached because EmbedQuery
	// returns an error first (sub-context cancelled at 5s).
	sem := &SemanticDeps{
		Client: client,
		Store:  embeddings.NewStore(nil),
	}

	// Parent context with 30s — generous, well above the 6s assertion threshold.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result := semanticSuggest(ctx, sem, "/repo", "render_coturn", "")
	elapsed := time.Since(start)

	// The result must be empty (EmbedQuery timed out → no suggestions).
	if result != "" {
		t.Errorf("expected empty result (embed timed out), got: %s", result)
	}

	// Without Fix #2: elapsed ≈ 10s (full server delay, parent ctx not bounded).
	// With Fix #2:    elapsed ≈ 5s  (semanticFallbackEmbedTimeout sub-context).
	// Assertion budget: 6s = 5s timeout + 1s epsilon for HTTP round-trip overhead.
	if elapsed >= 6*time.Second {
		t.Errorf("semanticSuggest took %v, expected < 6s — Fix #2 "+
			"(semanticFallbackEmbedTimeout sub-context) is missing", elapsed)
	}
}
