package goanalysis

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// waitTimeout bounds every assertion below that depends on the fix's
// correctness (as opposed to `entered`/ctx-deadline waits, which fire
// regardless of whether the fix is present). Generous relative to the 5ms
// budgets used here so it never flakes under load, but short enough that a
// reverted fix fails fast with a clear message instead of hanging for the
// test binary's full default timeout.
const waitTimeout = 2 * time.Second

// swapLoadPackagesFn overrides the package-level loadPackagesFn for the
// duration of a test and restores it on cleanup. White-box only (package
// goanalysis, not goanalysis_test) — see loadPackagesFn's doc comment for
// why the seam exists: a real LoadPackages call shells out to `go list` and
// can't be paused at a chosen instant, so proving the ctx-budget races below
// deterministically requires a barrier-controlled fake instead of guessed
// sleep durations against real subprocess timing.
func swapLoadPackagesFn(t *testing.T, fn func(ctx context.Context, root string, opts LoadOpts) (*LoadResult, error)) {
	t.Helper()
	orig := loadPackagesFn
	loadPackagesFn = fn
	t.Cleanup(func() { loadPackagesFn = orig })
}

// blockingLoader returns a loadPackagesFn replacement that signals `entered`
// as soon as it starts (proving the shared load is genuinely in flight) and
// then blocks until `release` is closed, at which point it returns a
// trivially-valid *LoadResult. This lets a test hold the shared singleflight
// load open for exactly as long as it needs to exercise a ctx-budget race,
// with zero reliance on wall-clock sleeps for correctness. Ignores ctx
// entirely — this stands in for the fact that a real go/packages.Load call
// is a black box the CALLER can't peek into; the ctx-budget contract being
// tested belongs to CachedLoadPackages' own waiter-selection logic, not to
// the loader function it wraps.
func blockingLoader(entered, release chan struct{}) func(context.Context, string, LoadOpts) (*LoadResult, error) {
	return func(_ context.Context, _ string, _ LoadOpts) (*LoadResult, error) {
		close(entered)
		<-release
		return &LoadResult{Packages: nil}, nil
	}
}

// TestCachedLoadPackages_LongBudgetFollowerSurvivesShortBudgetLeader is
// direction 1 of the go-code PR #294 pr-review-council round-2 HIGH:
// singleflight.Do previously fanned the LEADER's ctx (and only the leader's
// ctx) to every waiter, so a short-ctx leader's expired budget silently
// handed its own premature ctx-cancel failure to a long-ctx follower that
// still had plenty of budget left — exactly the symptom that starved
// extractGoImplements' IMPLEMENTS edges when a shorter CALLS probe raced it
// against the same root.
//
// RED guarantee: revert CachedLoadPackages' body to coalesce via
// loadGroup.Do(root, func() (any, error) { return loadPackagesFn(ctx, ...) })
// under the caller's own ctx and fan that single (val, err) tuple to every
// waiter (singleflight's pre-fix shape) — the leader's own goroutine then
// runs the closure SYNCHRONOUSLY inside Do, so it stays blocked on
// blockingLoader's <-release past its own ctx deadline (nothing selects on
// ctx.Done() anymore), and the follower's Do call blocks on the leader's
// WaitGroup with no ctx awareness at all. Both waits below then hit their
// waitTimeout branch and fail — the follower never gets a chance to observe
// the correct decoupled behavior because the leader itself never returns
// early.
func TestCachedLoadPackages_LongBudgetFollowerSurvivesShortBudgetLeader(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	swapLoadPackagesFn(t, blockingLoader(entered, release))
	// closeRelease is safe to call more than once (sync.OnceFunc): the test
	// closes it explicitly mid-body once it has proven the follower is still
	// waiting, but must also guarantee the background load unblocks (rather
	// than leaking past the test) if an assertion fails before that point.
	closeRelease := sync.OnceFunc(func() { close(release) })
	defer closeRelease()

	root := t.TempDir() // unique key per test; content irrelevant (loader is faked)

	// Leader: short budget. Starts the shared load and becomes the
	// singleflight "leader" by virtue of arriving first.
	leaderDone := make(chan struct{})
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer shortCancel()
	go func() {
		defer close(leaderDone)
		_, _ = CachedLoadPackages(shortCtx, root)
	}()

	<-entered // the shared load is now genuinely in flight, blocked on `release`

	// Follower: long (effectively unbounded) budget, joins the same
	// in-flight load while it is still blocked.
	type followerResult struct {
		result *LoadResult
		err    error
	}
	followerCh := make(chan followerResult, 1)
	go func() {
		r, err := CachedLoadPackages(context.Background(), root)
		followerCh <- followerResult{r, err}
	}()

	<-shortCtx.Done() // deterministically wait for the leader's own budget to expire

	// Under the fix, the leader returns promptly (its own ctx.Err()) without
	// waiting for the still-blocked shared load.
	select {
	case <-leaderDone:
	case <-time.After(waitTimeout):
		t.Fatal("leader (short ctx) did not return at its own deadline — it appears to " +
			"be blocked synchronously inside the shared load instead of selecting on its own ctx.Done()")
	}

	// The shared load is STILL blocked on `release` at this point — the
	// follower cannot have received a result yet unless it were (incorrectly)
	// handed the leader's premature failure.
	select {
	case fr := <-followerCh:
		t.Fatalf("follower returned before the shared load was released — "+
			"got a premature result (result=%v, err=%v) instead of waiting for "+
			"its own long budget", fr.result, fr.err)
	default:
		// expected: follower is still waiting on the shared load.
	}

	closeRelease() // let the shared load complete

	select {
	case fr := <-followerCh:
		if fr.err != nil {
			t.Fatalf("expected the long-budget follower to get a VALID result even "+
				"though the short-budget leader's ctx expired mid-load, got error: %v", fr.err)
		}
		if fr.result == nil {
			t.Fatal("expected a non-nil *LoadResult for the follower")
		}
	case <-time.After(waitTimeout):
		t.Fatal("follower never returned after the shared load was released (deadlock)")
	}
}

// TestCachedLoadPackages_ShortBudgetFollowerReturnsBeforeSlowLeaderFinishes
// is direction 2 of the same HIGH: a short-ctx follower joining a slow
// leader's in-flight load must return at ITS OWN deadline, not block for the
// leader's full duration — the SLA a synchronous caller (e.g. the 10s warm
// probe in EnrichWithTypedResolution) depends on when it races a much slower
// shared load (e.g. the 15-minute background GOCACHE warm) against the same
// root.
//
// RED guarantee: revert CachedLoadPackages to the pre-fix loadGroup.Do
// shape described above — the short-budget follower's CachedLoadPackages
// call blocks synchronously on the leader's WaitGroup with no ctx
// selection, so it cannot return before the leader's fn does (i.e. before
// `release` closes); the first `select` below hits its waitTimeout branch
// and fails.
func TestCachedLoadPackages_ShortBudgetFollowerReturnsBeforeSlowLeaderFinishes(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	swapLoadPackagesFn(t, blockingLoader(entered, release))
	defer close(release) // always unblock the background load so it can't leak past the test

	root := t.TempDir()

	// Leader: long/unbounded budget (simulates the 15-minute background warm).
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _ = CachedLoadPackages(context.Background(), root)
	}()

	<-entered // the shared load is now genuinely in flight, blocked on `release`

	// Follower: short budget (simulates the 10s synchronous warm probe),
	// joins while the shared load is still blocked and will remain blocked
	// for the rest of this test (release is only closed on defer, after all
	// assertions run).
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer shortCancel()

	followerErrCh := make(chan error, 1)
	go func() {
		_, err := CachedLoadPackages(shortCtx, root)
		followerErrCh <- err
	}()

	select {
	case err := <-followerErrCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected the short-budget follower to fail with its OWN "+
				"context.DeadlineExceeded (not the leader's outcome), got: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("short-budget follower did not return at its own deadline — it blocked " +
			"for (at least) the slow leader's full in-flight duration instead")
	}

	// The leader must still be in flight — proves the follower's prompt
	// return was genuinely due to honoring its OWN ctx, not some side effect
	// that also unblocked (or corrupted) the leader's shared load.
	select {
	case <-leaderDone:
		t.Fatal("leader unexpectedly finished before `release` was closed — " +
			"the shared load must not be tied to the follower's ctx")
	default:
		// expected: leader still blocked on `release`.
	}
}

// callerMarkerKey is a private context key used only by
// TestCachedLoadPackages_SharedLoadRunsUnderOwnDecoupledCtx to detect
// value-propagation from a caller's ctx into the shared load's ctx.
type callerMarkerKey struct{}

// TestCachedLoadPackages_SharedLoadRunsUnderOwnDecoupledCtx is the MED fix
// from the go-code PR #294 pr-review-council round-2 report
// (reviews/pr-council/pr-294-2026-07-01.md): cacheTTLFor previously
// classified a load's outcome using whichever CALLER's ctx triggered it, so
// a caller with a budget larger than LoadPackages' internal defaultTimeout
// cap (e.g. the 15-minute background GOCACHE warm vs. the internal 10m cap)
// could see ctx.Err()==nil at the moment the internal cap fired, and the
// resulting timeout was misclassified as a genuine repo-level failure
// (negative-cached) instead of a budget timeout (never cached).
//
// Rather than waiting out a real ~10-minute timeout to reproduce that race,
// this proves the STRUCTURAL property that makes the fix correct by
// construction: the ctx (and LoadOpts.Timeout) handed to the shared loader
// — and therefore to cacheTTLFor — is the shared load's OWN object, built
// fresh with its own bounded deadline and zero value-propagation from
// whichever caller happened to trigger it. Once that holds, NO caller's
// outer budget (10m, 15m, or unbounded) can ever reach cacheTTLFor, so the
// >10m-outer-budget mismatch this MED describes cannot recur regardless of
// how long any individual caller's ctx lives.
//
// RED guarantee: revert the singleflight closure to call
// loadPackagesFn(ctx, root, LoadOpts{}) using the CALLER's ctx directly
// (dropping the loadCtx := context.WithTimeout(context.Background(), ...)
// indirection) and every assertion below fails: gotCtx == callerCtx, the
// value propagates, and gotOpts.Timeout is the LoadOpts{} zero value (0),
// not defaultTimeout.
func TestCachedLoadPackages_SharedLoadRunsUnderOwnDecoupledCtx(t *testing.T) {
	callerCtx := context.WithValue(context.Background(), callerMarkerKey{}, "marker")

	var gotCtx context.Context
	var gotOpts LoadOpts
	captured := make(chan struct{})
	swapLoadPackagesFn(t, func(ctx context.Context, _ string, opts LoadOpts) (*LoadResult, error) {
		gotCtx = ctx
		gotOpts = opts
		close(captured)
		return &LoadResult{}, nil
	})

	root := t.TempDir()
	if _, err := CachedLoadPackages(callerCtx, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-captured:
	case <-time.After(waitTimeout):
		t.Fatal("loadPackagesFn was never invoked")
	}

	if gotCtx == callerCtx {
		t.Fatal("expected the shared load to run under its OWN ctx object, not the " +
			"caller's ctx directly — classification (cacheTTLFor) must never see a " +
			"caller-supplied ctx")
	}
	if gotCtx.Value(callerMarkerKey{}) != nil {
		t.Fatal("expected the shared load's ctx to carry NO value propagated from the " +
			"caller — any propagation would mean the shared ctx is still coupled to " +
			"whichever caller happened to trigger the load")
	}
	if _, hasDeadline := gotCtx.Deadline(); !hasDeadline {
		t.Fatal("expected the shared load's ctx to carry its own bounded deadline, " +
			"independent of any caller's budget")
	}
	if gotOpts.Timeout != defaultTimeout {
		t.Fatalf("expected LoadOpts.Timeout to be threaded through as defaultTimeout so "+
			"LoadPackages' internal cap and the shared ctx's deadline coincide by "+
			"construction, got %v", gotOpts.Timeout)
	}
}
