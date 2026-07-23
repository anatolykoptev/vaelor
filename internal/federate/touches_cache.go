package federate

import (
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	touchesCacheTTL     = 10 * time.Minute
	touchesCacheMaxSize = 10
)

// federatedCoChangeCacheSize is the live entry count of the process-global
// touchesCache. The cache is bounded (LRU maxSize=10) and TTL-expired (10m), so
// this gauge should stay ≤ 10 in steady state. A sustained value at the cap
// means the poll pattern is fanning out across >10 repos (working set larger
// than the cache); a value stuck at 0 while federated_cochange is being called
// would signal the cache is bypassed. Makes the #608 memory-growth fix
// observable on /metrics.
var federatedCoChangeCacheSize = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gocode_federated_cochange_cache_size",
		Help: "Live entry count of the process-global federated co-change touches cache (bounded LRU cap=10, TTL=10m, #608).",
	},
)

type touchesCacheEntry struct {
	data []RepoTouch
	at   time.Time
}

// touchesCache is a small TTL+LRU cache for per-repo collectTouches results.
// git log --name-only takes 5-30s on large repos; caching avoids re-running it
// on repeated federated_cochange calls (poll pattern).
type touchesCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, touchesCacheEntry]
	// onMutate, when set, is called under mu after every cache mutation (set,
	// TTL-eviction) with the new entry count. Set only on globalTouchesCache to
	// publish the size gauge; test-local instances leave it nil so they do not
	// mutate the process-global gauge.
	onMutate func(size int)
}

// globalTouchesCache is the process-level touches cache shared across calls.
var globalTouchesCache = &touchesCache{
	lru: cache.NewLRU[string, touchesCacheEntry](touchesCacheMaxSize),
}

func init() {
	// Wire the size gauge to the production cache only.
	globalTouchesCache.onMutate = func(size int) {
		federatedCoChangeCacheSize.Set(float64(size))
	}
}

func (c *touchesCache) get(key string) ([]RepoTouch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > touchesCacheTTL {
		c.lru.Delete(key)
		if c.onMutate != nil {
			c.onMutate(c.lru.Len())
		}
		return nil, false
	}
	return e.data, true
}

func (c *touchesCache) set(key string, data []RepoTouch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, touchesCacheEntry{data: data, at: time.Now()})
	if c.onMutate != nil {
		c.onMutate(c.lru.Len())
	}
}

// touchesCacheKey produces a stable per-repo cache key.
func touchesCacheKey(root string) string {
	return "touches::" + root
}

// IsRepoWarm reports whether repo root has a live (non-expired) touches entry.
func IsRepoWarm(root string) bool {
	_, ok := globalTouchesCache.get(touchesCacheKey(root))
	return ok
}

// WarmTouches returns the cached touches for root, or nil when the cache is cold.
// Returns a copy; callers may append to the result safely.
func WarmTouches(root string) []RepoTouch {
	data, ok := globalTouchesCache.get(touchesCacheKey(root))
	if !ok {
		return nil
	}
	out := make([]RepoTouch, len(data))
	copy(out, data)
	return out
}
