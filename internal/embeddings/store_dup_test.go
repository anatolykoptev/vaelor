package embeddings

import (
	"context"
	"testing"
)

// TestSimilarPairHasKindFields is a compile-time + behavioral assertion that
// SimilarPair carries KindA and KindB. If the fields are absent the test will
// not compile, providing a RED signal before implementation.
func TestSimilarPairHasKindFields(t *testing.T) {
	p := SimilarPair{
		SymbolA:    "Foo",
		FileA:      "a.go",
		LineA:      10,
		KindA:      "function",
		SymbolB:    "Bar",
		FileB:      "b.go",
		LineB:      20,
		KindB:      "method",
		Similarity: 0.97,
	}
	if p.KindA != "function" {
		t.Errorf("KindA = %q, want %q", p.KindA, "function")
	}
	if p.KindB != "method" {
		t.Errorf("KindB = %q, want %q", p.KindB, "method")
	}
}

// TestSymbolRefFields asserts SymbolRef carries the expected fields.
// RED: SymbolRef is declared in store_dup.go (not yet written).
func TestSymbolRefFields(t *testing.T) {
	ref := SymbolRef{
		FilePath:   "pkg/foo.go",
		SymbolName: "DoThing",
		SymbolKind: "function",
		StartLine:  42,
	}
	if ref.FilePath != "pkg/foo.go" {
		t.Errorf("FilePath = %q, want %q", ref.FilePath, "pkg/foo.go")
	}
	if ref.SymbolName != "DoThing" {
		t.Errorf("SymbolName = %q, want %q", ref.SymbolName, "DoThing")
	}
	if ref.SymbolKind != "function" {
		t.Errorf("SymbolKind = %q, want %q", ref.SymbolKind, "function")
	}
	if ref.StartLine != 42 {
		t.Errorf("StartLine = %d, want %d", ref.StartLine, 42)
	}
}

// TestExactDupPairFields asserts ExactDupPair carries A, B SymbolRef and BodyHash.
// RED: ExactDupPair is declared in store_dup.go (not yet written).
func TestExactDupPairFields(t *testing.T) {
	pair := ExactDupPair{
		A: SymbolRef{
			FilePath:   "x/a.go",
			SymbolName: "Alpha",
			SymbolKind: "function",
			StartLine:  1,
		},
		B: SymbolRef{
			FilePath:   "y/b.go",
			SymbolName: "Beta",
			SymbolKind: "function",
			StartLine:  5,
		},
		BodyHash: 0xdeadbeef,
	}
	if pair.A.SymbolName != "Alpha" {
		t.Errorf("A.SymbolName = %q, want %q", pair.A.SymbolName, "Alpha")
	}
	if pair.B.SymbolName != "Beta" {
		t.Errorf("B.SymbolName = %q, want %q", pair.B.SymbolName, "Beta")
	}
	if pair.BodyHash != 0xdeadbeef {
		t.Errorf("BodyHash = %d, want %d", pair.BodyHash, int64(0xdeadbeef))
	}
}

// TestMaxExactDupPairsConst asserts the limit const is exported-or-accessible
// and has a reasonable (non-zero, bounded) value.
// RED: maxExactDupPairs is declared in store_dup.go (not yet written).
func TestMaxExactDupPairsConst(t *testing.T) {
	const want = 200
	if maxExactDupPairs != want {
		t.Errorf("maxExactDupPairs = %d, want %d", maxExactDupPairs, want)
	}
}

// TestFindExactDuplicatesCompiles is a compile-time + call-path assertion that
// FindExactDuplicates exists on *Store with signature
//
//	func(*Store) FindExactDuplicates(context.Context, string) ([]ExactDupPair, error)
//
// A nil-pool Store is used intentionally: pgxpool.(*Pool).Exec panics on a nil
// pool, so the deferred recover catches that panic to prove the call reached the
// pgxpool boundary (i.e., EnsureSchema and the method body compiled and ran up
// to that point). This is NOT a functional DB test; real-DB behavior is covered
// by Phase 4's integration harness.
//
// FindSimilarPairs has the same nil-pool panic profile — no guard is needed or
// desired; production never builds a Store with a nil pool.
func TestFindExactDuplicatesCompiles(t *testing.T) {
	defer func() { _ = recover() }() // swallow nil-pool panic from pgxpool

	s := &Store{} // zero Store: nil pool
	// The two-return-value assignment verifies the signature at compile time.
	_, _ = s.FindExactDuplicates(t.Context(), "some-repo")
}

// ---------------------------------------------------------------------------
// Tests for FindNearDuplicates (Phase 5) — RED before implementation.
// ---------------------------------------------------------------------------

// TestCanonicalPairKey verifies canonical pair ordering: the endpoint with the
// lexicographically smaller "file:symbol" string is always assigned to the A
// side of a SimilarPair produced by nearDupCanonicalPair. This is the dedup
// invariant that prevents the same pair appearing twice (once from each endpoint).
func TestCanonicalPairKey(t *testing.T) {
	// nearDupCanonicalPair is the pure helper; it must build a SimilarPair with
	// A <= B ordering and Similarity = 1 - distance.
	cases := []struct {
		fileA, symA, fileB, symB string
		distance                 float32
		wantFileA, wantSymA      string // expected A endpoint (the lesser one)
		wantFileB, wantSymB      string // expected B endpoint (the greater one)
	}{
		{
			// "a.go:Foo" < "b.go:Bar" — A stays A.
			fileA: "a.go", symA: "Foo", fileB: "b.go", symB: "Bar",
			distance:  0.10,
			wantFileA: "a.go", wantSymA: "Foo",
			wantFileB: "b.go", wantSymB: "Bar",
		},
		{
			// "z.go:Foo" > "a.go:Bar" — A and B must flip so the lesser is A.
			fileA: "z.go", symA: "Foo", fileB: "a.go", symB: "Bar",
			distance:  0.05,
			wantFileA: "a.go", wantSymA: "Bar",
			wantFileB: "z.go", wantSymB: "Foo",
		},
		{
			// Similarity = 1 - distance must be correct.
			fileA: "c.go", symA: "X", fileB: "d.go", symB: "Y",
			distance:  0.15,
			wantFileA: "c.go", wantSymA: "X",
			wantFileB: "d.go", wantSymB: "Y",
		},
	}

	for _, tc := range cases {
		p := nearDupCanonicalPair(
			tc.fileA, tc.symA, 1, "function",
			tc.fileB, tc.symB, 2, "method",
			tc.distance,
		)
		if p.FileA != tc.wantFileA || p.SymbolA != tc.wantSymA {
			t.Errorf("A endpoint: got %s:%s, want %s:%s",
				p.FileA, p.SymbolA, tc.wantFileA, tc.wantSymA)
		}
		if p.FileB != tc.wantFileB || p.SymbolB != tc.wantSymB {
			t.Errorf("B endpoint: got %s:%s, want %s:%s",
				p.FileB, p.SymbolB, tc.wantFileB, tc.wantSymB)
		}
		wantSim := float32(1) - tc.distance
		if p.Similarity != wantSim {
			t.Errorf("Similarity = %f, want %f (1 - distance %f)",
				p.Similarity, wantSim, tc.distance)
		}
	}
}

// TestNearDupPairKeyString verifies nearDupPairKey produces the same canonical
// "file:symbol|file:symbol" key regardless of which endpoint is passed first —
// the deduplication map relies on this property.
func TestNearDupPairKeyString(t *testing.T) {
	k1 := nearDupPairKey("a.go", "Foo", "b.go", "Bar")
	k2 := nearDupPairKey("b.go", "Bar", "a.go", "Foo")
	if k1 != k2 {
		t.Errorf("pair key not symmetric: %q vs %q", k1, k2)
	}

	// A self-pair must produce a recognisable (non-empty) key.
	kSelf := nearDupPairKey("a.go", "Foo", "a.go", "Foo")
	if kSelf == "" {
		t.Error("self-pair key must be non-empty")
	}
}

// TestDefaultNearDupK asserts the named constant exists and is positive.
func TestDefaultNearDupK(t *testing.T) {
	if defaultNearDupK <= 0 {
		t.Errorf("defaultNearDupK = %d, want > 0", defaultNearDupK)
	}
}

// TestFindNearDuplicatesCompiles is a compile-time + call-path assertion that
// FindNearDuplicates exists on *Store with signature:
//
//	func(*Store) FindNearDuplicates(ctx, repoKey string, k int, maxDist float32) (NearDupResult, error)
//
// A nil-pool Store is used; the deferred recover swallows the pgxpool panic to
// prove the method compiled and started executing (up to the DB boundary).
func TestFindNearDuplicatesCompiles(t *testing.T) {
	defer func() { _ = recover() }()

	s := &Store{}
	// Verifies return type and signature.
	res, _ := s.FindNearDuplicates(context.Background(), "repo", defaultNearDupK, 0.20)
	// res.Pairs must be accessible (not a bare []SimilarPair).
	_ = res.Pairs
	_ = res.SearchErrors
}
