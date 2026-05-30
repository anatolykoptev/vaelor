package compare

import (
	"sync"
	"time"
)

const (
	churnCacheTTL     = 10 * time.Minute
	churnCacheMaxSize = 5
)

type churnCacheEntry struct {
	data map[string]ChurnStats
	at   time.Time
}

// churnCache is a small TTL+LRU cache for CollectChurn results.
// git log --numstat takes ~5s on large repos; caching avoids re-running it
// when multiple tools analyze the same repo in quick succession.
type churnCache struct {
	mu      sync.Mutex
	entries map[string]*churnCacheEntry
	order   []string // insertion order for LRU eviction
}

var globalChurnCache = &churnCache{
	entries: make(map[string]*churnCacheEntry),
}

func (c *churnCache) get(key string) (map[string]ChurnStats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > churnCacheTTL {
		delete(c.entries, key)
		c.removeFromOrder(key)
		return nil, false
	}
	return e.data, true
}

func (c *churnCache) set(key string, data map[string]ChurnStats) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entry if at capacity.
	if len(c.entries) >= churnCacheMaxSize {
		if len(c.order) > 0 {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
	}

	// Remove existing entry from order if it exists (re-insertion updates order).
	c.removeFromOrder(key)

	c.entries[key] = &churnCacheEntry{data: data, at: time.Now()}
	c.order = append(c.order, key)
}

// removeFromOrder removes key from the insertion-order slice (called under lock).
func (c *churnCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// churnCacheKey derives a cache key from repo root + history window.
func churnCacheKey(root string, since time.Duration) string {
	return root + "\x00" + since.String()
}
