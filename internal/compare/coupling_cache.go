package compare

import (
	"fmt"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
)

const (
	couplingCacheTTL     = 10 * time.Minute
	couplingCacheMaxSize = 5
)

type couplingCacheEntry struct {
	data []CoupledPair
	at   time.Time
}

// couplingCache is a small TTL+LRU cache for CollectCoupling results.
// git log --name-only takes ~5s on large repos; caching avoids re-running it
// when multiple tools analyze the same repo in quick succession.
type couplingCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, couplingCacheEntry]
}

var globalCouplingCache = &couplingCache{
	lru: cache.NewLRU[string, couplingCacheEntry](couplingCacheMaxSize),
}

func (c *couplingCache) get(key string) ([]CoupledPair, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > couplingCacheTTL {
		c.lru.Delete(key)
		return nil, false
	}
	return e.data, true
}

func (c *couplingCache) set(key string, data []CoupledPair) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, couplingCacheEntry{data: data, at: time.Now()})
}

// couplingCacheKey produces a stable cache key from the repo root path and minCoChanges.
// minCoChanges is included because different thresholds produce different result sets.
func couplingCacheKey(root string, minCoChanges int) string {
	return fmt.Sprintf("coupling::%s::%d", root, minCoChanges)
}
