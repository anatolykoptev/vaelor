package semhealth

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// ---------------------------------------------------------------------------
// PairKey canonicalization (integration with embeddings.NewPairKey)
// ---------------------------------------------------------------------------

// TestPairKeyOf verifies that pairKeyOf produces a canonical key from a SimilarPair
// using the pair's file+symbol endpoints.
// RED: fails before pairKeyOf and embeddings.NewPairKey exist.
func TestPairKeyOf(t *testing.T) {
	p := embeddings.SimilarPair{
		FileA: "b.go", SymbolA: "Z",
		FileB: "a.go", SymbolB: "A",
	}
	k := pairKeyOf(p)
	if k.A != "a.go:A" {
		t.Errorf("k.A = %q, want %q", k.A, "a.go:A")
	}
	if k.B != "b.go:Z" {
		t.Errorf("k.B = %q, want %q", k.B, "b.go:Z")
	}
}

// ---------------------------------------------------------------------------
// filterSameFile
// ---------------------------------------------------------------------------

func TestFilterSameFile_DropsWhenSameFile(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "pkg/foo.go", SymbolA: "FuncA", FileB: "pkg/foo.go", SymbolB: "FuncB"},
		{FileA: "pkg/foo.go", SymbolA: "FuncA", FileB: "pkg/bar.go", SymbolB: "FuncC"},
	}
	kept, dropped := filterSameFile(pairs, false)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("len(kept) = %d, want 1", len(kept))
	}
	if kept[0].FileB != "pkg/bar.go" {
		t.Errorf("kept wrong pair: %+v", kept[0])
	}
}

func TestFilterSameFile_KeepsWhenIncludeSameFile(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "pkg/foo.go", SymbolA: "FuncA", FileB: "pkg/foo.go", SymbolB: "FuncB"},
	}
	kept, dropped := filterSameFile(pairs, true)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("len(kept) = %d, want 1", len(kept))
	}
}

func TestFilterSameFile_EmptyInput(t *testing.T) {
	kept, dropped := filterSameFile(nil, false)
	if dropped != 0 || len(kept) != 0 {
		t.Errorf("empty input: kept=%v dropped=%d", kept, dropped)
	}
}

// ---------------------------------------------------------------------------
// filterTests
// ---------------------------------------------------------------------------

func TestFilterTests_DropsTestFiles(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		// A is a test file — should be dropped.
		{FileA: "store_test.go", SymbolA: "TestFoo", FileB: "store.go", SymbolB: "Foo"},
		// B is a test file — should be dropped.
		{FileA: "service.go", SymbolA: "Handle", FileB: "service_test.go", SymbolB: "TestHandle"},
		// Neither is a test file — must be kept.
		{FileA: "impl/foo.go", SymbolA: "Compute", FileB: "impl/bar.go", SymbolB: "Calculate"},
	}
	kept, dropped := filterTests(pairs)
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("len(kept) = %d, want 1", len(kept))
	}
	if kept[0].SymbolA != "Compute" {
		t.Errorf("wrong pair kept: %+v", kept[0])
	}
}

func TestFilterTests_KeepsImplPairs(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "a.go", SymbolA: "A", FileB: "b.go", SymbolB: "B"},
	}
	kept, dropped := filterTests(pairs)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("len(kept) = %d, want 1", len(kept))
	}
}

// ---------------------------------------------------------------------------
// filterKind
// ---------------------------------------------------------------------------

func TestFilterKind_DropsLowSignalKinds(t *testing.T) {
	tests := []struct {
		name     string
		kindA    string
		kindB    string
		wantDrop bool
	}{
		{"field/field", "field", "field", true},
		{"var/var", "var", "var", true},
		{"const/const", "const", "const", true},
		{"import/import", "import", "import", true},
		{"field/function", "field", "function", true}, // either side low-signal → drop
		{"function/field", "function", "field", true}, // either side low-signal → drop
		{"function/function", "function", "function", false},
		{"method/method", "method", "method", false},
		{"function/method", "function", "method", false}, // cross-kind: the key target case
		{"method/function", "method", "function", false}, // same pair, reversed order
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pairs := []embeddings.SimilarPair{
				{FileA: "a.go", SymbolA: "X", KindA: tt.kindA,
					FileB: "b.go", SymbolB: "Y", KindB: tt.kindB},
			}
			_, dropped := filterKind(pairs)
			gotDrop := dropped > 0
			if gotDrop != tt.wantDrop {
				t.Errorf("kindA=%q kindB=%q: dropped=%d, wantDrop=%v",
					tt.kindA, tt.kindB, dropped, tt.wantDrop)
			}
		})
	}
}

func TestFilterKind_EmptyInput(t *testing.T) {
	kept, dropped := filterKind(nil)
	if dropped != 0 || len(kept) != 0 {
		t.Errorf("empty: kept=%v dropped=%d", kept, dropped)
	}
}

// ---------------------------------------------------------------------------
// filterCallsEdges (graph filter via injected interface)
// ---------------------------------------------------------------------------

// fakeGraphFilter is a test double for graphPairFilter.
type fakeGraphFilter struct {
	connectedByCallsFn  func(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error)
	interfaceSiblingsFn func(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error)
}

func (f *fakeGraphFilter) PairsConnectedByCalls(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
	if f.connectedByCallsFn != nil {
		return f.connectedByCallsFn(ctx, graphName, pairs)
	}
	return map[embeddings.PairKey]bool{}, nil
}

func (f *fakeGraphFilter) PairsSharingInterface(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
	if f.interfaceSiblingsFn != nil {
		return f.interfaceSiblingsFn(ctx, graphName, pairs)
	}
	return map[embeddings.PairKey]bool{}, nil
}

// TestFilterCallsEdges_DropsConnectedPairs asserts that a pair marked as
// "connected by CALLS" in the fake is dropped and others are kept.
func TestFilterCallsEdges_DropsConnectedPairs(t *testing.T) {
	caller := embeddings.SimilarPair{
		FileA: "a.go", SymbolA: "Caller", KindA: "function",
		FileB: "b.go", SymbolB: "Callee", KindB: "function",
		Similarity: 0.93,
	}
	genuineDup := embeddings.SimilarPair{
		FileA: "x.go", SymbolA: "Parse", KindA: "function",
		FileB: "y.go", SymbolB: "ParseV2", KindB: "function",
		Similarity: 0.95,
	}
	pairs := []embeddings.SimilarPair{caller, genuineDup}

	callerKey := pairKeyOf(caller)
	fake := &fakeGraphFilter{
		connectedByCallsFn: func(_ context.Context, _ string, _ []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
			return map[embeddings.PairKey]bool{callerKey: true}, nil
		},
	}

	kept, dropped := filterCallsEdges(context.Background(), fake, "testrepo", pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(kept) != 1 {
		t.Fatalf("len(kept) = %d, want 1", len(kept))
	}
	if kept[0].SymbolA != "Parse" {
		t.Errorf("wrong pair kept: %+v", kept[0])
	}
}

// TestFilterCallsEdges_ErrorDropsNothing asserts that when the graph filter
// returns an error, no pairs are dropped (graceful degradation).
func TestFilterCallsEdges_ErrorDropsNothing(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "a.go", SymbolA: "Foo", FileB: "b.go", SymbolB: "Bar"},
	}
	fake := &fakeGraphFilter{
		connectedByCallsFn: func(_ context.Context, _ string, _ []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
			return nil, errGraphUnavailable
		},
	}
	kept, dropped := filterCallsEdges(context.Background(), fake, "testrepo", pairs)
	if dropped != 0 {
		t.Errorf("error path: dropped = %d, want 0 (graceful degradation)", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("error path: len(kept) = %d, want 1", len(kept))
	}
}

// TestFilterCallsEdges_NilFilterDropsNothing asserts that a nil graphPairFilter
// is a no-op (graph unavailable path).
func TestFilterCallsEdges_NilFilterDropsNothing(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "a.go", SymbolA: "Foo", FileB: "b.go", SymbolB: "Bar"},
	}
	kept, dropped := filterCallsEdges(context.Background(), nil, "testrepo", pairs)
	if dropped != 0 {
		t.Errorf("nil filter: dropped = %d, want 0", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("nil filter: len(kept) = %d, want 1", len(kept))
	}
}

// ---------------------------------------------------------------------------
// filterInterfaceSiblings (graph filter via injected interface)
// ---------------------------------------------------------------------------

// TestFilterInterfaceSiblings_DropsSiblings asserts that the "Search quartet"
// false-positive class (four methods implementing the same interface) is dropped.
func TestFilterInterfaceSiblings_DropsSiblings(t *testing.T) {
	// Simulate two Search implementations flagged as similar (interface siblings).
	sibling := embeddings.SimilarPair{
		FileA: "fts/search.go", SymbolA: "Search", KindA: "method",
		FileB: "vector/search.go", SymbolB: "Search", KindB: "method",
		Similarity: 0.94,
	}
	// A genuine dup that has nothing to do with the interface.
	genuineDup := embeddings.SimilarPair{
		FileA: "x.go", SymbolA: "Render", KindA: "function",
		FileB: "y.go", SymbolB: "RenderHTML", KindB: "function",
		Similarity: 0.96,
	}
	pairs := []embeddings.SimilarPair{sibling, genuineDup}
	siblingKey := pairKeyOf(sibling)

	fake := &fakeGraphFilter{
		interfaceSiblingsFn: func(_ context.Context, _ string, _ []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
			return map[embeddings.PairKey]bool{siblingKey: true}, nil
		},
	}

	kept, dropped := filterInterfaceSiblings(context.Background(), fake, "testrepo", pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(kept) != 1 {
		t.Fatalf("len(kept) = %d, want 1", len(kept))
	}
	if kept[0].SymbolA != "Render" {
		t.Errorf("wrong pair kept: %+v", kept[0])
	}
}

// TestFilterInterfaceSiblings_ErrorDropsNothing asserts graceful degradation.
func TestFilterInterfaceSiblings_ErrorDropsNothing(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "a.go", SymbolA: "Foo", FileB: "b.go", SymbolB: "Bar"},
	}
	fake := &fakeGraphFilter{
		interfaceSiblingsFn: func(_ context.Context, _ string, _ []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
			return nil, errGraphUnavailable
		},
	}
	kept, dropped := filterInterfaceSiblings(context.Background(), fake, "testrepo", pairs)
	if dropped != 0 {
		t.Errorf("error path: dropped = %d, want 0 (graceful)", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("error path: len(kept) = %d, want 1", len(kept))
	}
}

// TestFilterInterfaceSiblings_NilFilterDropsNothing asserts nil is a no-op.
func TestFilterInterfaceSiblings_NilFilterDropsNothing(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{FileA: "a.go", SymbolA: "Foo", FileB: "b.go", SymbolB: "Bar"},
	}
	kept, dropped := filterInterfaceSiblings(context.Background(), nil, "testrepo", pairs)
	if dropped != 0 {
		t.Errorf("nil filter: dropped = %d, want 0", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("nil filter: len(kept) = %d, want 1", len(kept))
	}
}
