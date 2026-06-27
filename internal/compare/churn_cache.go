package compare

import (
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
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
	mu  sync.Mutex
	lru *cache.LRU[string, churnCacheEntry]
}

var globalChurnCache = &churnCache{
	lru: cache.NewLRU[string, churnCacheEntry](churnCacheMaxSize),
}

func (c *churnCache) get(key string) (map[string]ChurnStats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > churnCacheTTL {
		c.lru.Delete(key)
		return nil, false
	}
	return e.data, true
}

func (c *churnCache) set(key string, data map[string]ChurnStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, churnCacheEntry{data: data, at: time.Now()})
}

// churnCacheKey derives a cache key from repo root + history window.
func churnCacheKey(root string, since time.Duration) string {
	return root + "\x00" + since.String()
}
