package goanalysis

import (
	"context"
	"time"

	"github.com/anatolykoptev/vaelor/internal/cache"
	"golang.org/x/sync/singleflight"
)

const (
	// loadCacheMaxSize bounds CachedLoadPackages' in-memory footprint: each
	// entry holds a full type-checked package set (NeedDeps), so this stays
	// small on the load-disciplined 4-core ARM autoindex box. Mirrors
	// internal/callgraph/repo_cache.go's cgCacheMaxSize bound.
	loadCacheMaxSize = 8

	// loadCacheTTL is how long a SUCCESSFUL load stays fresh. Matches
	// callgraph's cgCacheTTL so an IMPLEMENTS pass (extractGoImplements) and
	// a CALLS pass (tryGoTypesResolution) against the same repo, run in
	// quick succession, land in the same reuse window.
	loadCacheTTL = 5 * time.Minute

	// loadCacheNegativeTTL is how long a GENUINE repo-level failure (missing
	// go.mod, unbuildable deps, indexer crash) is remembered before the next
	// caller is allowed to retry. Short on purpose: long enough to stop a
	// retry-storm when IMPLEMENTS and CALLS both hit the same broken repo
	// within one request burst, short enough that a transient failure
	// (GOCACHE still warming) self-heals within one burn-in cycle instead of
	// the full positive TTL.
	//
	// A caller-BUDGET-specific failure (context.DeadlineExceeded /
	// context.Canceled) is deliberately NOT covered by this TTL — see
	// cacheTTLFor.
	loadCacheNegativeTTL = 30 * time.Second
)

// loadCacheEntry holds a load result (or its failure).
type loadCacheEntry struct {
	result *LoadResult
	err    error
}

// packagesLoadCache is a small TTL+LRU cache for CachedLoadPackages, keyed
// by repo root. Built on cache.TTLLRU (internal/cache/ttl_lru.go), which
// generalizes the bounded-LRU-with-lazy-TTL-eviction shape already proven at
// internal/callgraph/repo_cache.go's cgCache, applied one layer down at the
// go/packages.Load boundary instead of the finished *CallGraph boundary —
// see cache.TTLLRU's doc comment for why this is the shared extraction
// point rather than a fifth hand-rolled copy.
var packagesLoadCache = cache.NewTTLLRU[string, loadCacheEntry](loadCacheMaxSize, loadCacheTTL)

// loadGroup coalesces concurrent CachedLoadPackages calls for the same root
// into a single in-flight load — see CachedLoadPackages.
var loadGroup singleflight.Group

// loadPackagesFn is the production go/packages loader the singleflight
// closure in CachedLoadPackages calls. Indirected through a package-level
// var (instead of calling LoadPackages directly) purely for testability:
// white-box tests in this package (goanalysis, not goanalysis_test) swap it
// for a barrier-controlled fake to deterministically exercise the
// per-waiter ctx-budget races fixed below — a real LoadPackages call shells
// out to `go list` and can't be paused at a chosen instant, so proving "a
// long-budget follower isn't starved/poisoned by a short-budget leader"
// without this seam would mean guessing sleep durations against real
// subprocess timing instead of closing a channel on cue.
var loadPackagesFn = LoadPackages

// cacheTTLFor classifies how long a load's outcome should be cached:
//
//   - success (err == nil): loadCacheTTL.
//   - a shared-load-BUDGET-specific failure — ctx (the shared load's own
//     context, built fresh in CachedLoadPackages' singleflight closure, NOT
//     any individual caller's context) is done (its own bounded deadline
//     elapsed) — this describes the shared load's own allotted budget
//     running out, not a property of the repo. Returns 0 ("never cache" —
//     see cache.TTLLRU.SetWithTTL) so the next call always re-attempts.
//   - any other (genuine repo-level) failure — missing go.mod, unbuildable
//     deps, indexer crash: loadCacheNegativeTTL.
//
// Classification reads ctx.Err() on the shared load's own context object
// rather than pattern-matching err via errors.Is(err, context.DeadlineExceeded):
// golang.org/x/tools/go/packages' underlying `go list` driver does not
// reliably propagate context.DeadlineExceeded through an unwrappable chain
// — its own error formatting ("packages.Load: err: context deadline
// exceeded: stderr: ...") loses the sentinel in practice (verified
// empirically against this toolchain: 0/8 probed timeouts satisfied
// errors.Is), so pattern-matching the error value would silently fail to
// classify the overwhelming majority of real timeouts. ctx.Err() on the
// original, un-wrapped context is always reliable — it is our own object,
// not something threaded through a third-party subprocess driver.
func cacheTTLFor(ctx context.Context, err error) time.Duration {
	switch {
	case err == nil:
		return loadCacheTTL
	case ctx.Err() != nil:
		return 0
	default:
		return loadCacheNegativeTTL
	}
}

// CachedLoadPackages wraps LoadPackages with a small bounded LRU + short TTL
// cache keyed by root, coalesced with singleflight, so a full go/types
// NeedDeps load for a given repo is paid at most once per TTL window
// regardless of how many callers ask for it — concurrently or in sequence.
// This is what makes "load once, reuse for IMPLEMENTS+CALLS" a property of
// the cache rather than something callers coordinate: extractGoImplements
// (IMPLEMENTS) and tryGoTypesResolution (CALLS) both route through this
// function, so whichever runs first against a repo warms it for the other,
// and concurrent callers racing a cold cache share the single in-flight
// load instead of each paying for their own.
//
// Failures are cached per cacheTTLFor: a genuine repo-level failure (no
// go.mod, unbuildable deps) is cached under loadCacheNegativeTTL so a
// persistently-broken repo doesn't retry-storm packages.Load on every
// enrichment call; a shared-load-budget-specific failure (the coalesced
// load's own bounded deadline elapsing) is never cached, so it can't
// poison a later, independently-budgeted attempt against the same root.
//
// Concurrency: each caller gets its OWN ctx honored independently, even
// though at most one go/packages load is ever in flight per root. On a
// cache miss, the underlying load runs under loadGroup.DoChan in a
// dedicated goroutine (singleflight's own semantics — DoChan does not run
// the closure on any specific caller's goroutine), bounded by a budget the
// closure allocates for ITSELF (defaultTimeout, fully decoupled from every
// caller's ctx — see the closure body). Every caller — whichever happened
// to trigger the load and every later joiner racing the same cold root —
// then independently selects between that shared result and its own
// ctx.Done(): a short-budget caller (e.g. the 10s synchronous warm probe in
// EnrichWithTypedResolution) returns its own ctx.Err() at its own deadline
// without waiting out a slower shared load (e.g. the 15-minute background
// GOCACHE warm, or a longer sibling caller's own load), and a long-budget
// caller (e.g. the 30s extractGoImplements IMPLEMENTS pass) is never handed
// a short-budget sibling's premature timeout — it either gets the shared
// load's real result or, on true failure, its own independent ctx.Err().
// The shared load itself is unaffected by any caller giving up early: it
// keeps running (up to its own bounded budget) and still populates the
// cache for the next caller, since it was never tied to a specific caller's
// cancellation in the first place.
//
// This replaces an earlier version of this function that ran the coalesced
// load directly under whichever caller's ctx happened to start it (the
// "leader") and fanned that leader's ctx and result to every other waiter
// unconditionally — see the go-code PR #294 pr-review-council round-2
// report (reviews/pr-council/pr-294-2026-07-01.md) for the two failure
// directions that caused: a short-budget leader's premature ctx-cancel
// error silently starving a long-budget follower's edges, and a
// short-budget follower blocking for a slow leader's FULL duration instead
// of its own deadline.
func CachedLoadPackages(ctx context.Context, root string) (*LoadResult, error) {
	if cached, ok := packagesLoadCache.Get(root); ok {
		return cached.result, cached.err
	}

	ch := loadGroup.DoChan(root, func() (any, error) {
		// The shared load gets its OWN bounded budget, built from
		// context.Background() rather than any caller's ctx — decoupling it
		// from whichever caller happened to trigger it (the singleflight
		// "leader") is the whole point: that caller's cancellation/deadline
		// must not be able to kill the load out from under other concurrent
		// waiters with more remaining budget of their own.
		//
		// defaultTimeout is LoadPackages' own zero-value fallback
		// (loader.go); threading it explicitly via LoadOpts.Timeout instead
		// of relying on that fallback makes loadCtx's deadline and the
		// deadline LoadPackages actually enforces coincide BY CONSTRUCTION
		// (both anchored to the same constant from the same instant),
		// rather than by the two independently happening to agree.
		loadCtx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		result, loadErr := loadPackagesFn(loadCtx, root, LoadOpts{Timeout: defaultTimeout})
		packagesLoadCache.SetWithTTL(root, loadCacheEntry{result: result, err: loadErr}, cacheTTLFor(loadCtx, loadErr))
		return result, loadErr
	})

	select {
	case res := <-ch:
		return res.Val.(*LoadResult), res.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InvalidateCachedLoad evicts any cached entry (success or failure) for
// root, forcing the next CachedLoadPackages(ctx, root) to run a fresh load.
//
// Used before a load that must not be short-circuited by a stale
// negative-cached failure — e.g. callgraph's background GOCACHE-warm retry,
// which deliberately re-attempts with a much longer timeout after an
// earlier quick synchronous probe already failed (and got negative-cached
// under loadCacheNegativeTTL). Without eviction, the patient retry would
// immediately replay the stale failure from the cache instead of running
// the fresh, now-likely-to-succeed load it exists to perform.
func InvalidateCachedLoad(root string) {
	packagesLoadCache.Delete(root)
}
