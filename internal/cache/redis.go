package cache

import (
	"context"
	"encoding/json"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
)

// Key generates a deterministic cache key from parts using FNV-128a.
func Key(parts ...string) string {
	return kitcache.Key(parts...)
}

// GenericCacheConfig configures a GenericCache.
type GenericCacheConfig struct {
	MaxSize  int
	TTL      time.Duration
	RedisURL string // optional — if empty, L1-only mode
}

// GenericCache is a tiered (L1 in-memory + optional L2 Redis) cache for any JSON-serializable type.
// Wraps go-kit/cache.Cache with JSON serialization for typed access.
type GenericCache[T any] struct {
	c *kitcache.Cache
}

// NewGenericCache creates a GenericCache with the given configuration.
func NewGenericCache[T any](cfg GenericCacheConfig) *GenericCache[T] {
	return &GenericCache[T]{
		c: kitcache.New(kitcache.Config{
			RedisURL:      cfg.RedisURL,
			Prefix:        "gc:",
			L1MaxItems:    cfg.MaxSize,
			L1TTL:         cfg.TTL,
			L2TTL:         cfg.TTL,
			JitterPercent: 0.1,
		}),
	}
}

// Get retrieves a value. Checks L1 first, then L2 (Redis). Promotes L2 hits to L1.
func (c *GenericCache[T]) Get(ctx context.Context, key string) (T, bool) {
	data, ok := c.c.Get(ctx, key)
	if !ok {
		var zero T
		return zero, false
	}
	var val T
	if err := json.Unmarshal(data, &val); err != nil {
		var zero T
		return zero, false
	}
	return val, true
}

// Set stores a value in L1 and optionally L2 (Redis).
func (c *GenericCache[T]) Set(ctx context.Context, key string, val T) {
	data, err := json.Marshal(val)
	if err != nil {
		return
	}
	c.c.Set(ctx, key, data)
}

// Stats returns cache statistics.
func (c *GenericCache[T]) Stats() Stats {
	s := c.c.Stats()
	return Stats{
		Hits:    s.L1Hits + s.L2Hits,
		Misses:  s.L1Misses + s.L2Misses,
		Entries: s.L1Size,
	}
}

// Close stops background cleanup and closes L2.
func (c *GenericCache[T]) Close() {
	c.c.Close()
}
