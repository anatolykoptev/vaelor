package compare

import (
	"fmt"
	"sync"
	"time"
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
	mu      sync.Mutex
	entries map[string]*couplingCacheEntry
	order   []string // insertion order for LRU eviction
}

var globalCouplingCache = &couplingCache{
	entries: make(map[string]*couplingCacheEntry),
}

func (c *couplingCache) get(key string) ([]CoupledPair, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > couplingCacheTTL {
		delete(c.entries, key)
		c.removeFromOrder(key)
		return nil, false
	}
	return e.data, true
}

func (c *couplingCache) set(key string, data []CoupledPair) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entry if at capacity.
	if len(c.entries) >= couplingCacheMaxSize {
		if len(c.order) > 0 {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
	}

	// Remove existing entry from order if it exists (re-insertion updates order).
	c.removeFromOrder(key)

	c.entries[key] = &couplingCacheEntry{data: data, at: time.Now()}
	c.order = append(c.order, key)
}

// removeFromOrder removes key from the insertion-order slice (called under lock).
func (c *couplingCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// couplingCacheKey produces a stable cache key from the repo root path and minCoChanges.
// minCoChanges is included because different thresholds produce different result sets.
func couplingCacheKey(root string, minCoChanges int) string {
	return fmt.Sprintf("coupling::%s::%d", root, minCoChanges)
}
