package federate

import (
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/cache"
)

const (
	touchesCacheTTL     = 10 * time.Minute
	touchesCacheMaxSize = 10
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
}

// globalTouchesCache is the process-level touches cache shared across calls.
var globalTouchesCache = &touchesCache{
	lru: cache.NewLRU[string, touchesCacheEntry](touchesCacheMaxSize),
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
		return nil, false
	}
	return e.data, true
}

func (c *touchesCache) set(key string, data []RepoTouch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, touchesCacheEntry{data: data, at: time.Now()})
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
