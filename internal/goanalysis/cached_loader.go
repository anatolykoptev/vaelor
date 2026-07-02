package goanalysis

import (
	"context"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
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
// into a single in-flight LoadPackages call — see CachedLoadPackages.
var loadGroup singleflight.Group

// cacheTTLFor classifies how long a load's outcome should be cached:
//
//   - success (err == nil): loadCacheTTL.
//   - a caller-BUDGET-specific failure — ctx (the caller's OWN context,
//     unwrapped) is done (deadline exceeded or explicitly canceled) — this
//     describes the CALLER's remaining budget at the moment it asked for
//     the load, not a property of the repo. A 10s synchronous caller timing
//     out must not stop a subsequent 30s caller against the same root from
//     getting its own full attempt. Returns 0 ("never cache" — see
//     cache.TTLLRU.SetWithTTL) so the next call, on whatever budget, always
//     re-attempts.
//   - any other (genuine repo-level) failure — missing go.mod, unbuildable
//     deps, indexer crash: loadCacheNegativeTTL.
//
// Classification reads ctx.Err() on the caller's OWN context object rather
// than pattern-matching err via errors.Is(err, context.DeadlineExceeded):
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
// enrichment call; a caller-budget-specific failure (context.DeadlineExceeded
// / context.Canceled) is never cached, so a shorter caller's expired budget
// can't starve a longer caller's own attempt against the same root.
//
// Concurrency note: when multiple callers race a cold cache for the same
// root, singleflight runs LoadPackages exactly once and fans the result out
// to every waiter — but that one call executes under the FIRST caller's
// ctx (the "leader"), so a later caller with a shorter ctx than the
// leader's will not see its own deadline independently enforced against
// that shared in-flight load. This is singleflight's standard caveat,
// accepted here because the TTL cache (not this coalescing) serves the
// overwhelming majority of calls once a root is warm.
func CachedLoadPackages(ctx context.Context, root string) (*LoadResult, error) {
	if cached, ok := packagesLoadCache.Get(root); ok {
		return cached.result, cached.err
	}

	v, err, _ := loadGroup.Do(root, func() (any, error) {
		result, loadErr := LoadPackages(ctx, root, LoadOpts{})
		packagesLoadCache.SetWithTTL(root, loadCacheEntry{result: result, err: loadErr}, cacheTTLFor(ctx, loadErr))
		return result, loadErr
	})
	return v.(*LoadResult), err
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
