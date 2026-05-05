package callgraph

import (
	"fmt"
	"sync"
	"time"
)

const (
	cgCacheTTL     = 5 * time.Minute
	cgCacheMaxSize = 5
)

// cgCacheEntry holds a cached CallGraph and when it was computed.
type cgCacheEntry struct {
	cg  *CallGraph
	at  time.Time
	key string // for LRU eviction
}

// callGraphCache is a small TTL+LRU cache for BuildFromRepo results.
// Parsing all repo files is expensive (15-60s); caching avoids re-parsing
// when multiple tools analyze the same repo in quick succession.
type callGraphCache struct {
	mu      sync.Mutex
	entries map[string]*cgCacheEntry
	order   []string // insertion order for LRU eviction
}

var cgCache = &callGraphCache{
	entries: make(map[string]*cgCacheEntry),
}

func (c *callGraphCache) get(key string) (*CallGraph, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > cgCacheTTL {
		delete(c.entries, key)
		c.removeFromOrder(key)
		return nil, false
	}
	return e.cg, true
}

func (c *callGraphCache) set(key string, cg *CallGraph) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entry if at capacity.
	if len(c.entries) >= cgCacheMaxSize {
		if len(c.order) > 0 {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
	}

	// Remove existing entry from order if it exists (re-insertion updates order).
	c.removeFromOrder(key)

	c.entries[key] = &cgCacheEntry{cg: cg, at: time.Now(), key: key}
	c.order = append(c.order, key)
}

// removeFromOrder removes key from the insertion-order slice (called under lock).
func (c *callGraphCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
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
	cgCache.entries = make(map[string]*cgCacheEntry)
	cgCache.order = nil
}
