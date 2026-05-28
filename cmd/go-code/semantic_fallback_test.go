package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// searchByNameSpy is a test double for symbolNameSearcher that records the
// arguments it was called with and returns a pre-configured result.
// Mirrors the countingFinder pattern from internal/semhealth/semhealth_test.go.
//
// The 5s sub-context timeout from PR #139 is removed in favor of pg_trgm's
// sub-millisecond response time.
type searchByNameSpy struct {
	// configured response
	results []embeddings.SearchResult
	err     error

	// recorded call arguments (set on first call)
	calledLanguage string
	calledLimit    int
	calledKeywords []string
	callCount      int
}

func (s *searchByNameSpy) SearchBySymbolName(
	_ context.Context,
	_ string,
	keywords []string,
	language string,
	limit int,
) ([]embeddings.SearchResult, error) {
	s.callCount++
	s.calledKeywords = keywords
	s.calledLanguage = language
	s.calledLimit = limit
	return s.results, s.err
}

// TestSemanticSuggestEmbedServerIndependence is the CORE WIN test.
// Construct SemanticDeps with sem.Client = nil (embed-server unreachable).
// The trigram path must still return suggestions because it never touches
// the embed client.
//
// Anti-tautology: drives production semanticSuggest, which calls
// symbolNameSearcher.SearchBySymbolName on the spy. If the embed-server
// dependency were still present, the nil-client guard would return "" before
// the spy is ever called, causing the test to fail.
//
// Case 1 from the task spec.
func TestSemanticSuggestEmbedServerIndependence(t *testing.T) {
	spy := &searchByNameSpy{
		results: []embeddings.SearchResult{
			{SymbolName: "render_xray", FilePath: "infra/render_xray.go", SymbolKind: "func", StartLine: 10, Distance: 0.3},
			{SymbolName: "render_caddy", FilePath: "infra/render_caddy.go", SymbolKind: "func", StartLine: 20, Distance: 0.4},
		},
	}

	// sem.Client intentionally nil — embed-server is "unreachable".
	// The new trigram path must proceed without it.
	sem := &SemanticDeps{
		Client:        nil, // no embed client
		storeSearcher: spy,
	}

	result := semanticSuggest(context.Background(), sem, "/repo", "render_coturn", "go")

	if result == "" {
		t.Fatal("expected non-empty suggestions when embed-server is nil but store spy returns results — embed-server dependency is NOT gone")
	}
	if !strings.Contains(result, "<semantic_suggestions>") {
		t.Errorf("result missing <semantic_suggestions> wrapper: %s", result)
	}
	if !strings.Contains(result, "render_xray") {
		t.Errorf("result missing fake symbol 'render_xray': %s", result)
	}
	if !strings.Contains(result, "render_caddy") {
		t.Errorf("result missing fake symbol 'render_caddy': %s", result)
	}
	if spy.callCount != 1 {
		t.Errorf("spy called %d times, want 1", spy.callCount)
	}
}

// TestSemanticSuggestEmptyStoreResult verifies that an empty result from the
// store yields an empty string (no spurious XML wrapper).
//
// Case 2 from the task spec.
func TestSemanticSuggestEmptyStoreResult(t *testing.T) {
	spy := &searchByNameSpy{results: nil, err: nil}
	sem := &SemanticDeps{storeSearcher: spy}

	result := semanticSuggest(context.Background(), sem, "/repo", "no_match_symbol", "")
	if result != "" {
		t.Errorf("expected empty string on empty store results, got: %s", result)
	}
}

// TestSemanticSuggestStoreError verifies that a store error yields an empty
// string (best-effort fallback degrades silently) and does not panic.
//
// Case 3 from the task spec.
func TestSemanticSuggestStoreError(t *testing.T) {
	spy := &searchByNameSpy{err: errors.New("connection refused")}
	sem := &SemanticDeps{storeSearcher: spy}

	result := semanticSuggest(context.Background(), sem, "/repo", "some_symbol", "")
	if result != "" {
		t.Errorf("expected empty string on store error, got: %s", result)
	}
}

// TestSemanticSuggestNilDeps verifies that nil sem or nil storeSearcher both
// return empty string without panic.
//
// Case 4 from the task spec.
func TestSemanticSuggestNilDeps(t *testing.T) {
	// nil sem pointer
	result := semanticSuggest(context.Background(), nil, "/repo", "sym", "")
	if result != "" {
		t.Errorf("nil sem: expected empty string, got: %s", result)
	}

	// non-nil sem but nil storeSearcher (no store configured)
	sem := &SemanticDeps{storeSearcher: nil}
	result = semanticSuggest(context.Background(), sem, "/repo", "sym", "")
	if result != "" {
		t.Errorf("nil storeSearcher: expected empty string, got: %s", result)
	}
}

// TestSemanticSuggestCallPropagatesArgs verifies that semanticSuggest passes
// the language filter and topK limit through to the store unchanged.
//
// Case 6 from the task spec.
func TestSemanticSuggestCallPropagatesArgs(t *testing.T) {
	spy := &searchByNameSpy{
		results: []embeddings.SearchResult{
			{SymbolName: "foo", FilePath: "foo.rs", SymbolKind: "fn", StartLine: 1, Distance: 0.25},
		},
	}
	sem := &SemanticDeps{storeSearcher: spy}

	wantLanguage := "rust"
	wantLimit := semanticFallbackTopK

	_ = semanticSuggest(context.Background(), sem, "/repo", "foo_bar", wantLanguage)

	if spy.callCount != 1 {
		t.Fatalf("spy called %d times, want 1", spy.callCount)
	}
	if spy.calledLanguage != wantLanguage {
		t.Errorf("language = %q, want %q", spy.calledLanguage, wantLanguage)
	}
	if spy.calledLimit != wantLimit {
		t.Errorf("limit = %d, want %d (semanticFallbackTopK)", spy.calledLimit, wantLimit)
	}
}
