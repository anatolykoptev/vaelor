package goanalysis_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

// TestCachedLoadPackages_SecondCallWithinTTLIsCacheHit is the P1 fitness
// function required by the design doc: "a 2nd CachedLoadPackages(ctx,root)
// within TTL is a cache hit -> exactly one underlying LoadPackages."
//
// LoadPackages always allocates a fresh *LoadResult on every call
// (loader.go: `result := &LoadResult{}`), so identical pointers across two
// CachedLoadPackages calls against the same root are only possible if the
// second call served the cached entry instead of invoking LoadPackages
// again — this proves "load once, reuse for IMPLEMENTS+CALLS" without
// mocking or hand-copying the production loader into the test.
//
// RED guarantee: strip the cache out of CachedLoadPackages (make it call
// LoadPackages unconditionally on every invocation) and this test fails —
// r1 and r2 become distinct pointers even though their contents are
// equivalent.
func TestCachedLoadPackages_SecondCallWithinTTLIsCacheHit(t *testing.T) {
	dir := makeTestModule(t)

	r1, err := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err != nil {
		t.Fatalf("first CachedLoadPackages: %v", err)
	}
	if r1 == nil || len(r1.Packages) == 0 {
		t.Fatal("expected a non-empty result from the first (cold) call")
	}

	r2, err := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err != nil {
		t.Fatalf("second CachedLoadPackages: %v", err)
	}

	if r1 != r2 {
		t.Error("expected the 2nd call within TTL to return the SAME *LoadResult " +
			"pointer as the 1st (cache hit); got distinct pointers, meaning the " +
			"underlying LoadPackages ran twice for the same root")
	}
}

// TestCachedLoadPackages_DifferentRootsDoNotCollide verifies the cache is
// keyed by root — two distinct modules must not share a cached LoadResult.
func TestCachedLoadPackages_DifferentRootsDoNotCollide(t *testing.T) {
	dir1 := makeTestModule(t)
	dir2 := makeTestModule(t)

	r1, err := goanalysis.CachedLoadPackages(context.Background(), dir1)
	if err != nil {
		t.Fatalf("CachedLoadPackages(dir1): %v", err)
	}
	r2, err := goanalysis.CachedLoadPackages(context.Background(), dir2)
	if err != nil {
		t.Fatalf("CachedLoadPackages(dir2): %v", err)
	}

	if r1 == r2 {
		t.Error("expected distinct roots to produce distinct cached *LoadResult values")
	}
}

// TestCachedLoadPackages_NoGoModCachesFailure verifies a failed load (no
// go.mod) is itself cached under the negative TTL — the 2nd call must still
// report the same failure, not panic or hang re-attempting packages.Load.
func TestCachedLoadPackages_NoGoModCachesFailure(t *testing.T) {
	dir := t.TempDir() // no go.mod written

	_, err1 := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err1 == nil {
		t.Fatal("expected error for missing go.mod on 1st call")
	}

	_, err2 := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err2 == nil {
		t.Fatal("expected error for missing go.mod on 2nd call (cached failure)")
	}
}
