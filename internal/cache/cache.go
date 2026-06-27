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
// ParseCache — caches *parser.ParseResult per file path.
// Invalidation: modTime or size changed. Eviction: LRU (access-order).
// ──────────────────────────────────────────────────────────────────

// ParseCache caches tree-sitter parse results keyed by absolute file path.
type ParseCache struct {
	mu     sync.Mutex
	lru    *LRU[string, parseCacheEntry]
	hits   int64
	misses int64
}

type parseCacheEntry struct {
	result  *parser.ParseResult
	modTime int64 // unix nano
	size    int64
}

// NewParseCache creates a parse cache with the given maximum entry count.
func NewParseCache(maxSize int) *ParseCache {
	if maxSize <= 0 {
		maxSize = DefaultParseCacheSize
	}
	return &ParseCache{
		lru: NewLRU[string, parseCacheEntry](maxSize),
	}
}

// Get returns a cached parse result if the file hasn't changed.
// Returns nil if not cached or stale (modTime/size mismatch).
func (c *ParseCache) Get(path string, modTime int64, size int64) *parser.ParseResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.lru.Get(path)
	if !ok {
		c.misses++
		return nil
	}

	if e.modTime != modTime || e.size != size {
		// Stale — remove and treat as miss.
		c.lru.Delete(path)
		c.misses++
		return nil
	}

	c.hits++
	return e.result
}

// Put stores a parse result. Evicts the least-recently-used entry if at capacity.
func (c *ParseCache) Put(path string, modTime int64, size int64, result *parser.ParseResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(path, parseCacheEntry{result: result, modTime: modTime, size: size})
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
