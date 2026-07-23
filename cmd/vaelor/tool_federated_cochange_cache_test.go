package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/coupling"
)

// TestFederatedCoChangeCache_BoundedByLRUAndTTL is the RED-on-revert guard for
// #608: federatedCoChangeCache was a raw sync.Map with no eviction → unbounded
// memory growth on long-running servers. The cache now uses the repo's existing
// LRU+TTL idiom (same as internal/federate/touchesCache and
// internal/callgraph/cgCache). This test asserts BOTH bounds:
//
//  1. LRU cap: inserting past federatedCoChangeCacheMaxSize evicts the oldest
//     entry — len stays at the cap. Falsify: revert to unbounded sync.Map →
//     len grows past the cap → RED.
//
//  2. TTL expiry: a stale entry (at older than federatedCoChangeCacheTTL) is
//     evicted on load. Falsify: revert the TTL check → load returns the stale
//     entry → RED.
func TestFederatedCoChangeCache_BoundedByLRUAndTTL(t *testing.T) {
	t.Run("lru_cap_evicts_oldest", func(t *testing.T) {
		federatedCoChangeCache.clear()
		t.Cleanup(federatedCoChangeCache.clear)

		for i := 0; i < federatedCoChangeCacheMaxSize+5; i++ {
			key := fmt.Sprintf("lru-key-%d", i)
			federatedCoChangeCache.store(key, &federatedCoChangeCacheEntry{
				result: &FederatedCoChangeResult{Pairs: []coupling.VerifiedPair{}},
				done:   true,
			})
		}
		if got := federatedCoChangeCache.len(); got != federatedCoChangeCacheMaxSize {
			t.Fatalf("cache len = %d, want %d (LRU cap must evict oldest; #608)", got, federatedCoChangeCacheMaxSize)
		}
		// The first 5 keys (oldest) must have been evicted; the last entry
		// inserted must still be present.
		if _, ok := federatedCoChangeCache.load("lru-key-0"); ok {
			t.Fatal("oldest entry should have been evicted by LRU cap")
		}
		if _, ok := federatedCoChangeCache.load(fmt.Sprintf("lru-key-%d", federatedCoChangeCacheMaxSize+4)); !ok {
			t.Fatal("newest entry must still be present after LRU eviction")
		}
	})

	t.Run("ttl_expires_stale_entry", func(t *testing.T) {
		federatedCoChangeCache.clear()
		t.Cleanup(federatedCoChangeCache.clear)

		federatedCoChangeCache.store("stale", &federatedCoChangeCacheEntry{
			result: &FederatedCoChangeResult{Pairs: []coupling.VerifiedPair{}},
			done:   true,
			at:     time.Now().Add(-federatedCoChangeCacheTTL - time.Millisecond),
		})
		if _, ok := federatedCoChangeCache.load("stale"); ok {
			t.Fatal("stale entry (past TTL) should have been evicted on load (#608)")
		}
	})

	t.Run("fresh_entry_is_served", func(t *testing.T) {
		federatedCoChangeCache.clear()
		t.Cleanup(federatedCoChangeCache.clear)

		federatedCoChangeCache.store("fresh", &federatedCoChangeCacheEntry{
			result: &FederatedCoChangeResult{Pairs: []coupling.VerifiedPair{}},
			done:   true,
		})
		e, ok := federatedCoChangeCache.load("fresh")
		if !ok {
			t.Fatal("fresh entry should be served")
		}
		if !e.done || e.result == nil {
			t.Fatal("fresh entry data corrupted")
		}
	})
}
