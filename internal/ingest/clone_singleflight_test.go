package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests exercise the per-localPath single-flight on CloneRepo's COLD
// clone path (the never-cloned branch). They swap runCloneFn for a counting
// fake so the number of real git invocations is observable and controllable
// without shelling out to git or touching the network.
//
// The same-key test uses a gate inside the fake so concurrent callers
// deterministically overlap inside runCloneFn — without the single-flight
// every caller enters the fake and blocks on the gate (count > 1); with the
// single-flight only the leader enters (count == 1) and the rest share its
// result.

// fakeCloneTree is the body of the counting fake runCloneFn: it materialises a
// valid, non-empty tree at dest so CloneResult.LocalPath points at something a
// caller can stat. Returns nil so CloneRepo treats the clone as successful.
func fakeCloneTree(dest string) error {
	if err := os.MkdirAll(dest, dirPerm); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, "SENTINEL"), []byte("ok\n"), 0o644)
}

// TestCloneRepo_ColdClone_SingleFlight_SameKeyClonesOnce: N=20 goroutines
// hitting the cold path for the SAME slug/localPath concurrently must invoke
// runCloneFn EXACTLY ONCE; all 20 receive the same valid non-empty result.
//
// RED on the pre-singleflight code (runCloneFn indirection alone): every
// goroutine enters the fake → cloneCount > 1.
// GREEN with the single-flight: only the leader enters → cloneCount == 1.
//
// NOTE: deliberately NOT t.Parallel — these tests swap the package-level
// runCloneFn seam. Non-parallel tests run in Go's sequential phase, finishing
// (and restoring runCloneFn via t.Cleanup) before the parallel tests in the
// other clone_*_test.go files (which call the real CloneRepo) resume, so the
// fake never leaks into them.
func TestCloneRepo_ColdClone_SingleFlight_SameKeyClonesOnce(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "workspace")

	var cloneCount int64
	gate := make(chan struct{})
	firstEntry := make(chan struct{}, 1)
	orig := runCloneFn
	t.Cleanup(func() { runCloneFn = orig })
	runCloneFn = func(_ context.Context, _, _ string, d string, _ []string) error {
		n := atomic.AddInt64(&cloneCount, 1)
		if n == 1 {
			select {
			case firstEntry <- struct{}{}:
			default:
			}
		}
		<-gate // block until the test releases the leader
		return fakeCloneTree(d)
	}

	opts := CloneOpts{Slug: "test/same", DestDir: dest, CloneURL: "file:///unused"}
	wantPath := filepath.Join(dest, "test_same")

	const N = 20
	type result struct {
		res *CloneResult
		err error
	}
	results := make([]result, N)
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			r, err := CloneRepo(context.Background(), opts)
			results[i] = result{r, err}
		}(i)
	}
	close(start) // release all N goroutines as simultaneously as possible

	// Wait for the leader to enter runCloneFn.
	select {
	case <-firstEntry:
	case <-time.After(5 * time.Second):
		t.Fatal("runCloneFn was never entered within 5s")
	}
	// Give the other N-1 goroutines a chance to race into the cold path.
	// Without the single-flight they all enter runCloneFn and block on gate.
	time.Sleep(100 * time.Millisecond)
	// Release the leader (and any racers when the single-flight is absent).
	close(gate)
	wg.Wait()

	if got := atomic.LoadInt64(&cloneCount); got != 1 {
		t.Fatalf("runClone invoked %d times for same key, want exactly 1 (single-flight failed)", got)
	}
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d error: %v", i, r.err)
		}
		if r.res == nil || r.res.LocalPath != wantPath {
			t.Fatalf("goroutine %d: want localPath %q, got %+v", i, wantPath, r.res)
		}
		if _, err := os.Stat(filepath.Join(wantPath, "SENTINEL")); err != nil {
			t.Fatalf("goroutine %d: SENTINEL missing — %v", i, err)
		}
	}
}

// TestCloneRepo_ColdClone_SingleFlight_DistinctKeysCloneIndependently: two
// distinct slugs resolve to two distinct localPaths and must each invoke
// runCloneFn once (no false serialization across keys).
func TestCloneRepo_ColdClone_SingleFlight_DistinctKeysCloneIndependently(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "workspace")

	var cloneCount int64
	orig := runCloneFn
	t.Cleanup(func() { runCloneFn = orig })
	runCloneFn = func(_ context.Context, _, _ string, d string, _ []string) error {
		atomic.AddInt64(&cloneCount, 1)
		return fakeCloneTree(d)
	}

	optsA := CloneOpts{Slug: "test/aaa", DestDir: dest, CloneURL: "file:///unused"}
	optsB := CloneOpts{Slug: "test/bbb", DestDir: dest, CloneURL: "file:///unused"}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = CloneRepo(context.Background(), optsA) }()
	go func() { defer wg.Done(); _, _ = CloneRepo(context.Background(), optsB) }()
	wg.Wait()

	if got := atomic.LoadInt64(&cloneCount); got != 2 {
		t.Fatalf("runClone invoked %d times for 2 distinct keys, want 2 (false serialization)", got)
	}
}

// TestCloneRepo_CancelledCtx_NeverStartsClone: a call made with an
// already-cancelled ctx must return ctx.Err() WITHOUT launching the
// single-flight clone. The shared clone runs on a decoupled ctx — once
// started it keeps writing into DestDir for up to cloneOpTimeout after the
// caller already returned, racing caller-owned DestDir cleanup (the
// t.TempDir() RemoveAll flake, #798/#734).
//
// RED on the pre-fix code (no early ctx.Err() check): the DoChan fn launches
// runCloneFn regardless of the caller's ctx; the counting fake records it →
// cloneCount > 0 → REDS.
func TestCloneRepo_CancelledCtx_NeverStartsClone(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "workspace")

	var cloneCount int64
	orig := runCloneFn
	t.Cleanup(func() { runCloneFn = orig })
	runCloneFn = func(_ context.Context, _, _ string, d string, _ []string) error {
		atomic.AddInt64(&cloneCount, 1)
		return fakeCloneTree(d)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CloneRepo(ctx, CloneOpts{Slug: "test/cancelled", DestDir: dest, CloneURL: "file:///unused"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}

	// The DoChan fn runs on its own goroutine; give it a generous window to
	// be scheduled. Under the fix cloneCount can NEVER become non-zero —
	// the fn is never submitted. A poll (not a point check) so a late-scheduled
	// fn under the broken code is still observed.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := atomic.LoadInt64(&cloneCount); got != 0 {
			t.Fatalf("runCloneFn invoked %d times with a pre-cancelled ctx — "+
				"the started clone would keep writing into DestDir after return (#798/#734)", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCloneRepo_ColdClone_SingleFlight_FlightReleasedForLaterCall: after a
// cold-clone flight completes, a LATER cold call for the same key must proceed
// (the flight key is released, not permanently stuck). The on-disk clone is
// wiped between calls to force the cold path again (otherwise the stat-hit
// refresh branch would handle it).
func TestCloneRepo_ColdClone_SingleFlight_FlightReleasedForLaterCall(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "workspace")

	var cloneCount int64
	orig := runCloneFn
	t.Cleanup(func() { runCloneFn = orig })
	runCloneFn = func(_ context.Context, _, _ string, d string, _ []string) error {
		atomic.AddInt64(&cloneCount, 1)
		return fakeCloneTree(d)
	}

	opts := CloneOpts{Slug: "test/later", DestDir: dest, CloneURL: "file:///unused"}
	wantPath := filepath.Join(dest, "test_later")

	if _, err := CloneRepo(context.Background(), opts); err != nil {
		t.Fatalf("first clone: %v", err)
	}
	if got := atomic.LoadInt64(&cloneCount); got != 1 {
		t.Fatalf("after first clone: count=%d, want 1", got)
	}

	// Wipe localPath to force the cold path again.
	if err := os.RemoveAll(wantPath); err != nil {
		t.Fatal(err)
	}

	if _, err := CloneRepo(context.Background(), opts); err != nil {
		t.Fatalf("second clone after wipe: %v", err)
	}
	if got := atomic.LoadInt64(&cloneCount); got != 2 {
		t.Fatalf("after second clone: count=%d, want 2 (flight key not released)", got)
	}
}
