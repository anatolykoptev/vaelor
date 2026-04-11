package compare_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/compare"
)

func TestCollectCoupling(t *testing.T) {
	// Use the go-code repo itself as test data.
	root := findRepoRoot(t)
	pairs := compare.CollectCoupling(context.Background(), root, 3)

	// go-code repo should have some coupled pairs.
	if len(pairs) == 0 {
		t.Log("no coupled pairs found (ok for repos with short history)")
		return
	}

	// Verify structure.
	for _, p := range pairs {
		if p.FileA == "" || p.FileB == "" {
			t.Errorf("empty file in pair: %+v", p)
		}
		if p.CoChanges < 3 {
			t.Errorf("coChanges < minCoChanges: %+v", p)
		}
		if p.Ratio <= 0 || p.Ratio > 1.0 {
			t.Errorf("ratio out of range: %+v", p)
		}
		if p.FileA >= p.FileB {
			t.Errorf("files not alphabetically ordered: %+v", p)
		}
	}

	// Verify sorted by coChanges descending.
	for i := 1; i < len(pairs); i++ {
		if pairs[i].CoChanges > pairs[i-1].CoChanges {
			t.Errorf("pairs not sorted: pairs[%d].CoChanges=%d > pairs[%d].CoChanges=%d",
				i, pairs[i].CoChanges, i-1, pairs[i-1].CoChanges)
		}
	}

	// Verify at most 20 pairs returned.
	if len(pairs) > 20 {
		t.Errorf("too many pairs: got %d, want <= 20", len(pairs))
	}

	t.Logf("found %d coupled pairs; top pair: %s <-> %s (%d co-changes, ratio=%.2f)",
		len(pairs), pairs[0].FileA, pairs[0].FileB, pairs[0].CoChanges, pairs[0].Ratio)
}
