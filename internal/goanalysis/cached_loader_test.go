package goanalysis_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
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

// TestCachedLoadPackages_DeadlineExceededNotCached is the H2 fix from the
// go-code PR #294 pr-review-council report (reviews/pr-council/
// pr-294-2026-07-01.md): a load that fails because the CALLER's context
// deadline expired must never be negative-cached, because the deadline
// describes that caller's budget, not a property of the repo. A shorter
// caller (e.g. the 10s synchronous probe in EnrichWithTypedResolution) must
// not starve a longer caller (e.g. the 30s extractGoImplements) against the
// same root by poisoning the cache with its own timeout.
//
// Deliberately does NOT assert on the 1st call's error VALUE (e.g. via
// errors.Is(err, context.DeadlineExceeded)) — empirically, golang.org/x/tools/
// go/packages' underlying `go list` driver does not reliably propagate
// context.DeadlineExceeded through an unwrappable chain (0/8 probed timeouts
// against this toolchain satisfied errors.Is), so the production fix itself
// classifies by ctx.Err() on the caller's own context object, not by
// pattern-matching err (see cacheTTLFor). This test exercises exactly that
// same reliable signal: an already-expired ctx passed into CachedLoadPackages.
//
// RED guarantee: before the fix, CachedLoadPackages cached every failure —
// including one produced by an expired ctx — under loadCacheNegativeTTL
// (30s), keyed only by root. The 2nd call here, made with a fresh unbounded
// context immediately after the 1st call's deadline expired, would replay
// the stale cached failure instead of re-attempting, and this test would
// fail with "expected 2nd call ... to succeed".
func TestCachedLoadPackages_DeadlineExceededNotCached(t *testing.T) {
	dir := makeTestModule(t)

	expiredCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // guarantee the deadline has passed

	_, err1 := goanalysis.CachedLoadPackages(expiredCtx, dir)
	if err1 == nil {
		t.Fatal("expected an error on the 1st call with an already-expired context")
	}

	// A 2nd call with a fresh, unexpired context must re-attempt — not
	// replay the 1st call's cached deadline failure.
	r2, err2 := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err2 != nil {
		t.Fatalf("expected the 2nd call (fresh budget) to succeed, not be poisoned by "+
			"the 1st call's cached deadline failure: %v", err2)
	}
	if r2 == nil || len(r2.Packages) == 0 {
		t.Fatal("expected a non-empty result from the 2nd (re-attempted) call")
	}
}

// TestCachedLoadPackages_GenuineFailureIsCached is the companion half of the
// H2 fix's acceptance criteria: unlike a caller-budget deadline, a GENUINE
// repo-level failure (here: no go.mod present) must still be cached under
// loadCacheNegativeTTL — proven by making dir a valid module AFTER the 1st
// failed call. If the failure were not cached, the 2nd call would find the
// now-valid go.mod and succeed; a cached failure means it doesn't.
func TestCachedLoadPackages_GenuineFailureIsCached(t *testing.T) {
	dir := t.TempDir() // no go.mod yet — a genuine repo-level failure

	_, err1 := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err1 == nil {
		t.Fatal("expected an error for missing go.mod on the 1st call")
	}

	// Make dir a valid module. A fresh (uncached) attempt would now succeed —
	// so a 2nd call that still errors proves the 1st failure was cached.
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/genuinefailure\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	_, err2 := goanalysis.CachedLoadPackages(context.Background(), dir)
	if err2 == nil {
		t.Fatal("expected the 2nd call to still report the cached genuine failure " +
			"(negative TTL not yet elapsed), even though dir is now a valid module — " +
			"got success, meaning the genuine failure was not cached")
	}
}

// TestCachedLoadPackages_ConcurrentCallersCoalesce is the M3 fitness
// function: N callers racing a COLD cache for the same root must see the
// underlying LoadPackages run at most once — every goroutine gets back the
// identical *LoadResult pointer, not N independently-loaded copies. This is
// the concurrency half of CachedLoadPackages' doc comment claim ("paid at
// most once per TTL window regardless of how many callers ask for it —
// concurrently or in sequence").
//
// RED guarantee: remove the singleflight.Group wrapper (call LoadPackages
// directly on every cache miss) and this test flakes/fails — concurrent
// goroutines racing the cold cache each load independently, producing
// distinct *LoadResult pointers instead of one shared pointer.
func TestCachedLoadPackages_ConcurrentCallersCoalesce(t *testing.T) {
	dir := makeTestModule(t)

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]*goanalysis.LoadResult, n)
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = goanalysis.CachedLoadPackages(context.Background(), dir)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("goroutine %d: expected the SAME *LoadResult pointer as goroutine 0 "+
				"(singleflight coalescing), got a distinct pointer", i)
		}
	}
}
