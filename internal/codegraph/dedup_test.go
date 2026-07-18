package codegraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-kit/rerank"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// TestDedupByFileSymbol_CollapsesIdenticalKey verifies that two SearchResults
// sharing the same FilePath+SymbolName are collapsed into one, keeping the
// lowest Distance.
//
// Anti-tautology: removing the dedup block from dedupByFileSymbol makes
// len(out)==2 (both entries survive), causing the len assertion to fail.
func TestDedupByFileSymbol_CollapsesIdenticalKey(t *testing.T) {
	in := []embeddings.SearchResult{
		{FilePath: "pkg/foo.go", SymbolName: "doThing", Distance: 0.5178, Source: "semantic"},
		{FilePath: "pkg/foo.go", SymbolName: "doThing", Distance: 0.6825, Source: "semantic"}, // dup, higher distance
	}
	out := dedupByFileSymbol(in, "ce_rerank")

	if len(out) != 1 {
		t.Fatalf("want 1 result after dedup, got %d", len(out))
	}
	if out[0].Distance != 0.5178 {
		t.Errorf("want lowest Distance 0.5178, got %v", out[0].Distance)
	}
}

// TestDedupByFileSymbol_DifferentSymbolsUnchanged verifies that distinct
// FilePath+SymbolName pairs are never collapsed.
//
// Anti-tautology: a dedup keyed only on FilePath (not SymbolName) would
// collapse "a.go:Foo" and "a.go:Bar", producing len(out)==2 — fails here.
func TestDedupByFileSymbol_DifferentSymbolsUnchanged(t *testing.T) {
	in := []embeddings.SearchResult{
		{FilePath: "a.go", SymbolName: "Foo", Distance: 0.3},
		{FilePath: "b.go", SymbolName: "Bar", Distance: 0.4},
		{FilePath: "a.go", SymbolName: "Baz", Distance: 0.5}, // same file, different symbol
	}
	out := dedupByFileSymbol(in, "ce_rerank")

	if len(out) != 3 {
		t.Fatalf("want 3 results (no dups), got %d", len(out))
	}
}

// TestDedupByFileSymbol_KeepsLowestDistance verifies that when the lower-distance
// entry appears SECOND, it still wins.
//
// Anti-tautology: swapping the Distance comparison to ">" instead of "<" keeps
// the higher distance — the Distance assertion fails.
func TestDedupByFileSymbol_KeepsLowestDistance(t *testing.T) {
	in := []embeddings.SearchResult{
		{FilePath: "x.go", SymbolName: "Run", Distance: 0.8, Source: "trigram"},
		{FilePath: "x.go", SymbolName: "Run", Distance: 0.2, Source: "semantic"}, // better match second
	}
	out := dedupByFileSymbol(in, "semantic_only")

	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Distance != 0.2 {
		t.Errorf("want Distance 0.2 (better match), got %v", out[0].Distance)
	}
	if out[0].Source != "semantic" {
		t.Errorf("want Source of winning entry %q, got %q", "semantic", out[0].Source)
	}
}

// TestDedupByFileSymbol_EmptyInput verifies the empty-slice fast path is
// handled without panic.
func TestDedupByFileSymbol_EmptyInput(t *testing.T) {
	out := dedupByFileSymbol(nil, "semantic_only")
	if len(out) != 0 {
		t.Fatalf("want 0, got %d", len(out))
	}
}

// TestRerankSemanticResults_DedupBeforeCE verifies that duplicate file:symbol
// pairs are collapsed before the CE reranker runs.
// Uses the cold-path reranker (no URL configured) so CE is bypassed but dedup
// still fires inside RerankSemanticResults.
//
// Anti-tautology: removing dedupByFileSymbol from RerankSemanticResults makes
// both copies survive cappedResults, returning len(out)==2 for one symbol.
func TestRerankSemanticResults_DedupBeforeCE(t *testing.T) {
	withRerankClient(t, rerank.New(rerank.Config{URL: ""}, nil))

	in := []embeddings.SearchResult{
		{FilePath: "pkg/auth.go", SymbolName: "Validate", Distance: 0.52, Source: "semantic"},
		{FilePath: "pkg/auth.go", SymbolName: "Validate", Distance: 0.68, Source: "semantic"}, // dup
	}
	out := RerankSemanticResults(context.Background(), "", "validate token", in, 10)

	if len(out) != 1 {
		t.Fatalf("want 1 result after dedup, got %d (dup survived)", len(out))
	}
	if out[0].Distance != 0.52 {
		t.Errorf("want lowest Distance 0.52, got %v", out[0].Distance)
	}
}
