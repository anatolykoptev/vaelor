package compare

import (
	"context"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestMatchExact_CanceledCtx_BailsPromptly verifies the #566 fix: matchExact
// (Pass 1, O(n×m)) must check ctx and bail when canceled, instead of running
// the full cross-product past the soft deadline. RED-on-revert: remove the
// ctx.Err() check in matchExact and this test takes several seconds (the full
// 20000×20000 scan runs to completion).
func TestMatchExact_CanceledCtx_BailsPromptly(t *testing.T) {
	// Build two large symbol sets (20000 each) so matchExact's O(n×m) would
	// take several seconds without the ctx check (400M comparisons), well
	// above the 500ms threshold — making the test RED-on-revert.
	a := make([]*parser.Symbol, 20000)
	b := make([]*parser.Symbol, 20000)
	for i := range a {
		a[i] = &parser.Symbol{Name: "funcA", Kind: parser.KindFunction, File: "/repo/a.go"}
	}
	for i := range b {
		b[i] = &parser.Symbol{Name: "funcB", Kind: parser.KindFunction, File: "/repo/b.go"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	t0 := time.Now()
	unmatchedA, unmatchedB, matches := matchExact(ctx, a, b)
	elapsed := time.Since(t0)

	// Must bail promptly — well under the time a full 400M-iteration scan takes.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("matchExact took %s on canceled ctx, want < 500ms (ctx check missing?)", elapsed)
	}
	// No exact matches (all names differ), so everything is unmatched.
	if len(matches) != 0 {
		t.Fatalf("want 0 matches, got %d", len(matches))
	}
	if len(unmatchedA) != 20000 {
		t.Fatalf("want 20000 unmatchedA, got %d", len(unmatchedA))
	}
	if len(unmatchedB) != 20000 {
		t.Fatalf("want 20000 unmatchedB, got %d", len(unmatchedB))
	}
}

// TestMatchExact_FastPath_AllMatched verifies the fast path (no ctx cancel)
// still matches all exact-name symbols — the ctx check does not perturb the
// result when the ctx is alive.
func TestMatchExact_FastPath_AllMatched(t *testing.T) {
	a := []*parser.Symbol{
		{Name: "foo", Kind: parser.KindFunction, File: "/repo/a.go"},
		{Name: "bar", Kind: parser.KindFunction, File: "/repo/a.go"},
	}
	b := []*parser.Symbol{
		{Name: "foo", Kind: parser.KindFunction, File: "/repo/b.go"},
		{Name: "bar", Kind: parser.KindFunction, File: "/repo/b.go"},
		{Name: "baz", Kind: parser.KindFunction, File: "/repo/b.go"},
	}

	unmatchedA, unmatchedB, matches := matchExact(context.Background(), a, b)
	if len(matches) != 2 {
		t.Fatalf("want 2 matches, got %d", len(matches))
	}
	if len(unmatchedA) != 0 {
		t.Fatalf("want 0 unmatchedA, got %d", len(unmatchedA))
	}
	if len(unmatchedB) != 1 {
		t.Fatalf("want 1 unmatchedB (baz), got %d", len(unmatchedB))
	}
}
