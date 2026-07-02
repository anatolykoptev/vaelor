package goanalysis

import (
	"context"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
)

const (
	// loadCacheMaxSize bounds CachedLoadPackages' in-memory footprint: each
	// entry holds a full type-checked package set (NeedDeps), so this stays
	// small on the load-disciplined 4-core ARM autoindex box. Mirrors
	// internal/callgraph/repo_cache.go's cgCacheMaxSize bound.
	loadCacheMaxSize = 8

	// loadCacheTTL is how long a SUCCESSFUL load stays fresh. Matches
	// callgraph's cgCacheTTL so an IMPLEMENTS pass (extractGoImplements) and
	// a CALLS pass (TryGoTypesResolution) against the same repo, run in
	// quick succession, land in the same reuse window.
	loadCacheTTL = 5 * time.Minute

	// loadCacheNegativeTTL is how long a FAILED load (missing go.mod, cold
	// GOCACHE timeout, broken deps) is remembered before the next caller is
	// allowed to retry. Short on purpose: long enough to stop a retry-storm
	// when IMPLEMENTS and CALLS both hit the same broken repo within one
	// request burst, short enough that a transient failure (GOCACHE still
	// warming) self-heals within one burn-in cycle instead of the full
	// positive TTL.
	loadCacheNegativeTTL = 30 * time.Second
)

// loadCacheEntry holds a load result (or its failure) and when it landed.
type loadCacheEntry struct {
	result *LoadResult
	err    error
	at     time.Time
}

// loadPackagesCache is a small TTL+LRU cache for CachedLoadPackages, keyed by
// repo root. Mirrors internal/callgraph/repo_cache.go's cgCache pattern —
// same bounded-LRU-with-lazy-TTL-eviction shape, applied one layer down at
// the go/packages.Load boundary instead of the finished *CallGraph boundary.
type loadPackagesCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, loadCacheEntry]
}

var packagesLoadCache = &loadPackagesCache{
	lru: cache.NewLRU[string, loadCacheEntry](loadCacheMaxSize),
}

// CachedLoadPackages wraps LoadPackages with a small bounded LRU + short TTL
// cache keyed by root, so a full go/types NeedDeps load for a given repo is
// paid at most once per TTL window regardless of how many callers ask for
// it. This is what makes "load once, reuse for IMPLEMENTS+CALLS" a property
// of the cache rather than something callers coordinate: extractGoImplements
// (IMPLEMENTS) and TryGoTypesResolution (CALLS) both route through this
// function, so whichever runs first against a repo warms it for the other.
//
// Failures are cached under loadCacheNegativeTTL (much shorter than the
// success TTL) so a persistently-broken repo (no go.mod, unbuildable deps)
// doesn't retry-storm packages.Load on every enrichment call, while a
// transient failure (cold GOCACHE) retries again soon.
func CachedLoadPackages(ctx context.Context, root string) (*LoadResult, error) {
	if cached, ok := packagesLoadCache.get(root); ok {
		return cached.result, cached.err
	}

	result, err := LoadPackages(ctx, root, LoadOpts{})
	packagesLoadCache.set(root, loadCacheEntry{result: result, err: err, at: time.Now()})
	return result, err
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
	packagesLoadCache.mu.Lock()
	defer packagesLoadCache.mu.Unlock()
	packagesLoadCache.lru.Delete(root)
}

func (c *loadPackagesCache) get(root string) (loadCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.lru.Get(root)
	if !ok {
		return loadCacheEntry{}, false
	}

	ttl := loadCacheTTL
	if e.err != nil {
		ttl = loadCacheNegativeTTL
	}
	if time.Since(e.at) > ttl {
		c.lru.Delete(root)
		return loadCacheEntry{}, false
	}
	return e, true
}

func (c *loadPackagesCache) set(root string, e loadCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(root, e)
}
