package callgraph

import (
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
)

// TestRepoCache_Behavior asserts TTL-expiry and capacity-eviction semantics
// for the callGraphCache backed by cache.LRU (migrated in PR #258).
func TestRepoCache_Behavior(t *testing.T) {
	t.Run("TTL expiry returns miss", func(t *testing.T) {
		c := &callGraphCache{
			lru: cache.NewLRU[string, cgCacheEntry](10),
		}

		// Insert with a timestamp already past the cache's TTL constant.
		c.mu.Lock()
		c.lru.Set("root", cgCacheEntry{
			cg: &CallGraph{},
			at: time.Now().Add(-cgCacheTTL - time.Millisecond),
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
		c := &callGraphCache{
			lru: cache.NewLRU[string, cgCacheEntry](10),
		}
		c.set("root", &CallGraph{})
		_, ok := c.get("root")
		if !ok {
			t.Fatal("expected cache hit within TTL")
		}
	})

	t.Run("capacity eviction keeps Len <= maxSize", func(t *testing.T) {
		const maxSize = 3
		c := &callGraphCache{
			lru: cache.NewLRU[string, cgCacheEntry](maxSize),
		}
		for i := range maxSize + 2 {
			input := TraceRepoInput{Root: "/repo" + string(rune('A'+i))}
			c.set(cgCacheKey(input), &CallGraph{})
		}
		c.mu.Lock()
		n := c.lru.Len()
		c.mu.Unlock()
		if n > maxSize {
			t.Fatalf("Len=%d exceeds maxSize=%d: eviction did not fire", n, maxSize)
		}
	})
}
