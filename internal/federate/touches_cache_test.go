package federate

import (
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/cache"
)

// TestTouchesCache_Behavior asserts TTL-expiry and capacity-eviction semantics
// for the touchesCache backed by cache.LRU (migrated in PR #258).
func TestTouchesCache_Behavior(t *testing.T) {
	t.Parallel()
	t.Run("TTL expiry returns miss", func(t *testing.T) {
		c := &touchesCache{
			lru: cache.NewLRU[string, touchesCacheEntry](10),
		}

		// Insert with a timestamp already past the cache's TTL constant.
		c.mu.Lock()
		c.lru.Set("repo::root", touchesCacheEntry{
			data: []RepoTouch{},
			at:   time.Now().Add(-touchesCacheTTL - time.Millisecond),
		})
		c.mu.Unlock()

		_, ok := c.get("repo::root")
		if ok {
			t.Fatal("expected TTL miss: entry inserted past TTL must not be returned")
		}
		// Confirm the stale entry was deleted (Len drops to 0).
		c.mu.Lock()
		n := c.lru.Len()
		c.mu.Unlock()
		if n != 0 {
			t.Fatalf("stale entry must be evicted on get; Len=%d", n)
		}
	})

	t.Run("get after set within TTL returns hit", func(t *testing.T) {
		c := &touchesCache{
			lru: cache.NewLRU[string, touchesCacheEntry](10),
		}
		c.set("repo::a", []RepoTouch{})
		_, ok := c.get("repo::a")
		if !ok {
			t.Fatal("expected cache hit within TTL")
		}
	})

	t.Run("capacity eviction keeps Len <= maxSize", func(t *testing.T) {
		const maxSize = 3
		c := &touchesCache{
			lru: cache.NewLRU[string, touchesCacheEntry](maxSize),
		}
		// Insert maxSize+2 entries — LRU must evict oldest.
		for i := range maxSize + 2 {
			key := touchesCacheKey("/repo" + string(rune('A'+i)))
			c.set(key, []RepoTouch{})
		}
		c.mu.Lock()
		n := c.lru.Len()
		c.mu.Unlock()
		if n > maxSize {
			t.Fatalf("Len=%d exceeds maxSize=%d: eviction did not fire", n, maxSize)
		}
	})
}
