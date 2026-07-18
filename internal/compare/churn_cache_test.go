package compare

import (
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/cache"
)

// TestChurnCache_Behavior asserts TTL-expiry and capacity-eviction semantics
// for the churnCache backed by cache.LRU (migrated in PR #258).
func TestChurnCache_Behavior(t *testing.T) {
	t.Run("TTL expiry returns miss", func(t *testing.T) {
		c := &churnCache{
			lru: cache.NewLRU[string, churnCacheEntry](10),
		}

		// Insert with a timestamp already past the cache's TTL constant.
		c.mu.Lock()
		c.lru.Set("root", churnCacheEntry{
			data: map[string]ChurnStats{},
			at:   time.Now().Add(-churnCacheTTL - time.Millisecond),
		})
		c.mu.Unlock()

		_, ok := c.get("root")
		if ok {
			t.Fatal("expected TTL miss: entry inserted past TTL must not be returned")
		}
		// Confirm stale entry was deleted.
		c.mu.Lock()
		n := c.lru.Len()
		c.mu.Unlock()
		if n != 0 {
			t.Fatalf("stale entry must be evicted on get; Len=%d", n)
		}
	})

	t.Run("get after set within TTL returns hit", func(t *testing.T) {
		c := &churnCache{
			lru: cache.NewLRU[string, churnCacheEntry](10),
		}
		c.set("root", map[string]ChurnStats{})
		_, ok := c.get("root")
		if !ok {
			t.Fatal("expected cache hit within TTL")
		}
	})

	t.Run("capacity eviction keeps Len <= maxSize", func(t *testing.T) {
		const maxSize = 3
		c := &churnCache{
			lru: cache.NewLRU[string, churnCacheEntry](maxSize),
		}
		for i := range maxSize + 2 {
			key := churnCacheKey("/repo"+string(rune('A'+i)), time.Hour)
			c.set(key, map[string]ChurnStats{})
		}
		c.mu.Lock()
		n := c.lru.Len()
		c.mu.Unlock()
		if n > maxSize {
			t.Fatalf("Len=%d exceeds maxSize=%d: eviction did not fire", n, maxSize)
		}
	})
}
