package embeddings

import (
	"testing"
)

// TestNewPairKey_Canonical verifies that PairKey always stores endpoints in
// lexicographic A < B order regardless of the call-site order.
// RED: fails before PairKey / NewPairKey exist.
func TestNewPairKey_Canonical(t *testing.T) {
	tests := []struct {
		name  string
		file1 string
		sym1  string
		file2 string
		sym2  string
		wantA string
		wantB string
	}{
		{
			name:  "already ordered",
			file1: "a.go", sym1: "A",
			file2: "b.go", sym2: "Z",
			wantA: "a.go:A", wantB: "b.go:Z",
		},
		{
			name:  "reversed — must flip",
			file1: "b.go", sym1: "Z",
			file2: "a.go", sym2: "A",
			wantA: "a.go:A", wantB: "b.go:Z",
		},
		{
			name:  "same file different symbols",
			file1: "pkg/foo.go", sym1: "Bbb",
			file2: "pkg/foo.go", sym2: "Aaa",
			wantA: "pkg/foo.go:Aaa", wantB: "pkg/foo.go:Bbb",
		},
		{
			name:  "equal endpoints canonical",
			file1: "x.go", sym1: "F",
			file2: "x.go", sym2: "F",
			wantA: "x.go:F", wantB: "x.go:F",
		},
		{
			name:  "colon in symbol name: split on last colon",
			file1: "a.go", sym1: "Recv:Method",
			file2: "b.go", sym2: "Recv:Method",
			wantA: "a.go:Recv:Method", wantB: "b.go:Recv:Method",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := NewPairKey(tt.file1, tt.sym1, tt.file2, tt.sym2)
			if k.A != tt.wantA {
				t.Errorf("A = %q, want %q", k.A, tt.wantA)
			}
			if k.B != tt.wantB {
				t.Errorf("B = %q, want %q", k.B, tt.wantB)
			}
		})
	}
}

// TestNewPairKey_ReversalProduceSameKey verifies that swapping argument order
// always yields the same PairKey (the canonical-order guarantee).
func TestNewPairKey_ReversalProduceSameKey(t *testing.T) {
	k1 := NewPairKey("b.go", "Z", "a.go", "A")
	k2 := NewPairKey("a.go", "A", "b.go", "Z")
	if k1 != k2 {
		t.Errorf("reversed args produced different keys: %+v vs %+v", k1, k2)
	}
}

// TestSymbolNamesFromPairs_UniqueSet verifies the name-extraction helper used
// inside PairsConnectedByCalls / PairsSharingInterface to build the name filter.
// This is a pure-function test with no DB dependency.
// RED: fails before symbolNamesFromPairs exists.
func TestSymbolNamesFromPairs_UniqueSet(t *testing.T) {
	pairs := []PairKey{
		{A: "a.go:Foo", B: "b.go:Bar"},
		{A: "b.go:Bar", B: "c.go:Baz"}, // Bar appears twice → must be deduplicated
	}
	names := symbolNamesFromPairs(pairs)
	want := map[string]bool{"Foo": true, "Bar": true, "Baz": true}
	if len(names) != len(want) {
		t.Errorf("got %d unique names, want %d: %v", len(names), len(want), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q in result", n)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing names: %v", want)
	}
}

// TestSymbolNameFromEndpoint verifies the last-colon split used to extract
// the symbol name from a "file:symbol" endpoint string.
// RED: fails before symbolNameFromEndpoint exists.
func TestSymbolNameFromEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"a.go:Foo", "Foo"},
		{"pkg/sub/bar.go:Handler", "Handler"},
		{"a.go:Recv:Method", "Method"}, // last colon
		{"nocoion", "nocoion"},         // no colon → whole string
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			got := symbolNameFromEndpoint(tt.endpoint)
			if got != tt.want {
				t.Errorf("symbolNameFromEndpoint(%q) = %q, want %q", tt.endpoint, got, tt.want)
			}
		})
	}
}
