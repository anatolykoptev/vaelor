package embeddings

import (
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
