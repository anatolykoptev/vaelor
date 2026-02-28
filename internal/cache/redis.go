package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyLength is the number of hex characters returned by Key (8 bytes = 16 hex chars).
const keyLength = 16

// Key generates a deterministic cache key from parts using SHA256 (first 8 bytes → 16 hex chars).
func Key(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}

// GenericCacheConfig configures a GenericCache.
type GenericCacheConfig struct {
	MaxSize  int
	TTL      time.Duration
	RedisURL string // optional — if empty, L1-only mode
}

// genericEntry is a single L1 cache entry.
type genericEntry[T any] struct {
	key       string
	value     T
	createdAt time.Time
}

// GenericCache is a tiered (L1 in-memory + optional L2 Redis) cache for any JSON-serializable type.
// L1 uses LRU eviction with TTL-based expiry. L2 is optional Redis; if RedisURL is empty or
// unreachable, the cache degrades gracefully to L1-only.
type GenericCache[T any] struct {
	mu      sync.Mutex
	entries map[string]*genericEntry[T]
	order   []string // insertion order, index 0 = oldest
	maxSize int
	ttl     time.Duration
	hits    int64
	misses  int64

	rdb *redis.Client // nil if L2 disabled
}

// NewGenericCache creates a GenericCache with the given configuration.
// If cfg.RedisURL is non-empty, attempts to connect to Redis for L2 caching.
// On connection failure, logs a warning and continues in L1-only mode.
func NewGenericCache[T any](cfg GenericCacheConfig) *GenericCache[T] {
	c := &GenericCache[T]{
		entries: make(map[string]*genericEntry[T], cfg.MaxSize),
		order:   make([]string, 0, cfg.MaxSize),
		maxSize: cfg.MaxSize,
		ttl:     cfg.TTL,
	}

	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			slog.Warn("cache: invalid redis URL, using L1-only", "err", err)
			return c
		}
		rdb := redis.NewClient(opts)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if pingErr := rdb.Ping(ctx).Err(); pingErr != nil {
			slog.Warn("cache: redis unreachable, using L1-only", "err", pingErr)
			_ = rdb.Close()
		} else {
			slog.Info("cache: redis L2 enabled", "url", cfg.RedisURL)
			c.rdb = rdb
		}
	}

	return c
}

// Get retrieves a value. Checks L1 first, then L2 (Redis). Promotes L2 hits to L1.
func (c *GenericCache[T]) Get(ctx context.Context, key string) (T, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok {
		if time.Since(entry.createdAt) > c.ttl {
			// Expired — remove from L1.
			c.removeEntryLocked(key)
			c.misses++
			c.mu.Unlock()
			var zero T
			return zero, false
		}
		c.hits++
		val := entry.value
		c.mu.Unlock()
		return val, true
	}
	c.mu.Unlock()

	// L1 miss — check L2.
	if c.rdb != nil {
		if val, found := c.getFromRedis(ctx, key); found {
			// Promote to L1.
			c.Set(ctx, key, val)
			return val, true
		}
	}

	c.mu.Lock()
	c.misses++
	c.mu.Unlock()
	var zero T
	return zero, false
}

// Set stores a value in L1 and optionally L2 (Redis).
func (c *GenericCache[T]) Set(ctx context.Context, key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry in place.
	if entry, ok := c.entries[key]; ok {
		entry.value = val
		entry.createdAt = time.Now()
		// Don't re-add to order; it keeps its existing position (not a strict LRU but
		// sufficient for TTL-based caches — oldest-insertion eviction).
		c.writeToRedisAsync(ctx, key, val)
		return
	}

	// Evict oldest entry if at capacity.
	for len(c.order) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[key] = &genericEntry[T]{
		key:       key,
		value:     val,
		createdAt: time.Now(),
	}
	c.order = append(c.order, key)

	c.writeToRedisAsync(ctx, key, val)
}

// Stats returns L1 cache statistics.
func (c *GenericCache[T]) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{
		Hits:    c.hits,
		Misses:  c.misses,
		Entries: len(c.entries),
	}
}

// removeEntryLocked removes a key from entries. Must be called with c.mu held.
func (c *GenericCache[T]) removeEntryLocked(key string) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// getFromRedis fetches and unmarshals a value from Redis.
func (c *GenericCache[T]) getFromRedis(ctx context.Context, key string) (T, bool) {
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		var zero T
		return zero, false
	}
	var val T
	if err := json.Unmarshal(data, &val); err != nil {
		slog.Warn("cache: failed to unmarshal redis value", "key", key, "err", err)
		var zero T
		return zero, false
	}
	return val, true
}

// writeToRedisAsync serializes val and sets it in Redis (best-effort, non-blocking).
// Called with c.mu held; spawns a goroutine to avoid blocking the caller.
func (c *GenericCache[T]) writeToRedisAsync(ctx context.Context, key string, val T) {
	if c.rdb == nil {
		return
	}
	rdb := c.rdb
	ttl := c.ttl
	go func() {
		data, err := json.Marshal(val)
		if err != nil {
			slog.Warn("cache: failed to marshal value for redis", "key", key, "err", err)
			return
		}
		if err := rdb.Set(ctx, key, data, ttl).Err(); err != nil {
			slog.Warn("cache: redis SET failed", "key", key, "err", err)
		}
	}()
}

// keyLength assertion — ensure Key returns exactly keyLength chars.
var _ = func() struct{} {
	k := Key("x")
	if len(k) != keyLength {
		panic(fmt.Sprintf("Key() returned %d chars, want %d", len(k), keyLength))
	}
	return struct{}{}
}()
