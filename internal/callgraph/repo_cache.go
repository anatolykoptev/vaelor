package callgraph

import (
	"fmt"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
)

const (
	cgCacheTTL     = 5 * time.Minute
	cgCacheMaxSize = 5
)

// cgCacheEntry holds a cached CallGraph and when it was computed.
type cgCacheEntry struct {
	cg *CallGraph
	at time.Time
}

// callGraphCache is a small TTL+LRU cache for BuildFromRepo results.
// Parsing all repo files is expensive (15-60s); caching avoids re-parsing
// when multiple tools analyze the same repo in quick succession.
type callGraphCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, cgCacheEntry]
}

var cgCache = &callGraphCache{
	lru: cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize),
}

func (c *callGraphCache) get(key string) (*CallGraph, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > cgCacheTTL {
		c.lru.Delete(key)
		return nil, false
	}
	return e.cg, true
}

func (c *callGraphCache) set(key string, cg *CallGraph) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, cgCacheEntry{cg: cg, at: time.Now()})
}

// cgCacheKey produces a stable cache key from TraceRepoInput fields
// that affect the result: root path, language filter, focus path, and
// the field-access opt-in (changes which edges land in the graph).
func cgCacheKey(input TraceRepoInput) string {
	return fmt.Sprintf("%s::%s::%s::fa=%t", input.Root, input.Language, input.Focus, input.IncludeFieldAccess)
}

// InvalidateBuildCache clears the entire BuildFromRepo cache.
// Used in tests and when a rebuild is explicitly requested.
func InvalidateBuildCache() {
	cgCache.mu.Lock()
	defer cgCache.mu.Unlock()
	cgCache.lru = cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize)
}
