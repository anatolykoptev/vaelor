package callgraph

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/go-code/internal/cache"
)

const (
	cgCacheTTL     = 5 * time.Minute
	cgCacheMaxSize = 5
)

// cgL2KeyVersion is embedded in the Redis key prefix so a wire-format or
// struct-shape change can invalidate stale L2 entries by bumping it.
const cgL2KeyVersion = "v1"
const cgL2Prefix = "gc:callgraph:" + cgL2KeyVersion + ":"

// cgCacheEntry holds a cached CallGraph and when it was computed.
type cgCacheEntry struct {
	cg *CallGraph
	at time.Time
}

// wireCGEntry is the gob-friendly on-wire format for an L2 entry.
type wireCGEntry struct {
	CG *CallGraph
	At time.Time
}

// callGraphCache is a small TTL+LRU cache for BuildFromRepo results.
// Parsing all repo files is expensive (15-60s); caching avoids re-parsing
// when multiple tools analyze the same repo in quick succession.
type callGraphCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, cgCacheEntry]
	l2  kitcache.L2
}

var cgCache = &callGraphCache{
	lru: cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize),
}

// SetL2 wires the process-level callgraph cache to Redis. Passing an empty
// redisURL disables L2. Called once from cmd/go-code/register.go at startup.
func SetL2(redisURL string) {
	var l2 kitcache.L2
	if redisURL != "" {
		l2 = kitcache.NewRedisL2(redisURL, 0, cgL2Prefix)
	}
	cgCache.mu.Lock()
	defer cgCache.mu.Unlock()
	cgCache.l2 = l2
}

func (c *callGraphCache) get(key string) (*CallGraph, bool) {
	c.mu.Lock()
	e, ok := c.lru.Get(key)
	if ok {
		if time.Since(e.at) <= cgCacheTTL {
			c.mu.Unlock()
			return e.cg, true
		}
		c.lru.Delete(key)
	}
	c.mu.Unlock()

	if c.l2 == nil {
		return nil, false
	}

	data, err := c.l2.Get(context.Background(), key)
	if err != nil {
		return nil, false
	}

	entry, err := decodeCGEntry(data)
	if err != nil {
		return nil, false
	}

	c.mu.Lock()
	c.lru.Set(key, cgCacheEntry{cg: entry.CG, at: entry.At})
	cg := entry.CG
	c.mu.Unlock()
	return cg, true
}

func (c *callGraphCache) set(key string, cg *CallGraph) {
	at := time.Now()

	c.mu.Lock()
	c.lru.Set(key, cgCacheEntry{cg: cg, at: at})
	l2 := c.l2
	c.mu.Unlock()

	if l2 != nil {
		if data, err := encodeCGEntry(cg, at); err == nil {
			_ = l2.Set(context.Background(), key, data, cgCacheTTL)
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
	cgCache.lru = cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize)
}

// encodeCGEntry serializes a CallGraph and timestamp to []byte using gob.
func encodeCGEntry(cg *CallGraph, at time.Time) ([]byte, error) {
	var buf bytes.Buffer
	w := wireCGEntry{CG: cg, At: at}
	if err := gob.NewEncoder(&buf).Encode(w); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeCGEntry inverts encodeCGEntry.
func decodeCGEntry(data []byte) (*wireCGEntry, error) {
	var w wireCGEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return nil, err
	}
	return &w, nil
}
