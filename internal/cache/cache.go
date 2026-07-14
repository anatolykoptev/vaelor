// Package cache provides in-memory caches for parsed ASTs and LLM responses.
//
// Both caches use LRU eviction and are safe for concurrent access.
package cache

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// Default cache sizes.
const (
	DefaultParseCacheSize = 5000
	DefaultLLMCacheSize   = 500
	DefaultLLMTTL         = time.Hour
)

// Stats holds cache hit/miss counters.
type Stats struct {
	Hits    int64 `json:"hits"`
	Misses  int64 `json:"misses"`
	Entries int   `json:"entries"`
}

// ──────────────────────────────────────────────────────────────────
// ParseCache — caches *parser.ParseResult plus its extracted
// []parser.CallSite, keyed by (file path, includeBody mode).
// Invalidation: modTime or size changed. Eviction: LRU (access-order).
// ──────────────────────────────────────────────────────────────────

// ParseCache caches tree-sitter parse results and call sites keyed by
// absolute file path and includeBody mode: a body-mode mismatch is always a
// miss, so both parse modes coexist independently in the same cache.
type ParseCache struct {
	mu     sync.Mutex
	lru    *LRU[parseCacheKey, parseCacheEntry]
	hits   int64
	misses int64
}

// parseCacheKey scopes a cache entry to a file path and the parse options
// it was produced with — different modes produce differently-shaped
// *parser.ParseResult values for the same path, so they must not collide.
type parseCacheKey struct {
	path            string
	includeBody     bool
	includeTypeRels bool
}

type parseCacheEntry struct {
	result  *parser.ParseResult
	calls   []parser.CallSite
	modTime int64 // unix nano
	size    int64
}

// NewParseCache creates a parse cache with the given maximum entry count.
func NewParseCache(maxSize int) *ParseCache {
	if maxSize <= 0 {
		maxSize = DefaultParseCacheSize
	}
	return &ParseCache{
		lru: NewLRU[parseCacheKey, parseCacheEntry](maxSize),
	}
}

// Get returns a cached parse result and its call sites for path, scoped to
// the given includeBody and includeTypeRels modes. Returns (nil, nil) if not
// cached, stale (modTime/size mismatch), or cached under a different mode.
func (c *ParseCache) Get(path string, modTime, size int64, includeBody, includeTypeRels bool) (*parser.ParseResult, []parser.CallSite) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := parseCacheKey{path: path, includeBody: includeBody, includeTypeRels: includeTypeRels}
	e, ok := c.lru.Get(key)
	if !ok {
		c.misses++
		return nil, nil
	}

	if e.modTime != modTime || e.size != size {
		// Stale — remove and treat as miss.
		c.lru.Delete(key)
		c.misses++
		return nil, nil
	}

	c.hits++
	return e.result, e.calls
}

// Put stores a parse result and its call sites for path, scoped to the given
// includeBody and includeTypeRels modes. Evicts the least-recently-used entry if at capacity.
func (c *ParseCache) Put(path string, modTime, size int64, includeBody, includeTypeRels bool, result *parser.ParseResult, calls []parser.CallSite) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := parseCacheKey{path: path, includeBody: includeBody, includeTypeRels: includeTypeRels}
	c.lru.Set(key, parseCacheEntry{result: result, calls: calls, modTime: modTime, size: size})
}

// Stats returns current cache statistics.
func (c *ParseCache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Hits: c.hits, Misses: c.misses, Entries: c.lru.Len()}
}

// ──────────────────────────────────────────────────────────────────
// LLMCache — caches LLM responses keyed by prompt hash.
// TTL-based expiry + S3-FIFO eviction via go-kit/cache.
// ──────────────────────────────────────────────────────────────────

// LLMCache caches LLM completion responses keyed by FNV-1a hash of prompts.
type LLMCache struct {
	c *kitcache.Cache
}

// NewLLMCache creates an LLM response cache with the given size and TTL.
func NewLLMCache(maxSize int, ttl time.Duration) *LLMCache {
	if maxSize <= 0 {
		maxSize = DefaultLLMCacheSize
	}
	if ttl <= 0 {
		ttl = DefaultLLMTTL
	}
	return &LLMCache{
		c: kitcache.New(kitcache.Config{
			L1MaxItems:    maxSize,
			L1TTL:         ttl,
			JitterPercent: 0,
		}),
	}
}

// PromptHash computes the FNV-1a hash of system + user prompt pair.
func PromptHash(systemPrompt, userPrompt string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(systemPrompt))
	_, _ = h.Write([]byte{0}) // null separator
	_, _ = h.Write([]byte(userPrompt))
	return h.Sum64()
}

// llmKey converts a uint64 hash to a hex string key for kitcache.
func llmKey(h uint64) string { return strconv.FormatUint(h, 16) }

// Get returns a cached LLM response if present and not expired.
func (c *LLMCache) Get(key uint64) (string, bool) {
	data, ok := c.c.Get(context.Background(), llmKey(key))
	if !ok {
		return "", false
	}
	return string(data), true
}

// Put stores an LLM response. Evicts the least-frequently-used entry if at capacity.
func (c *LLMCache) Put(key uint64, response string) {
	c.c.Set(context.Background(), llmKey(key), []byte(response))
}

// Stats returns current cache statistics.
func (c *LLMCache) Stats() Stats {
	s := c.c.Stats()
	return Stats{Hits: s.L1Hits, Misses: s.L1Misses, Entries: s.L1Size}
}

// Key generates a deterministic cache key from parts using FNV-128a.
func Key(parts ...string) string {
	return kitcache.Key(parts...)
}
