package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTLLRU_HitWithinTTL(t *testing.T) {
	c := NewTTLLRU[string, int](3, time.Hour)
	c.Set("a", 1)

	v, ok := c.Get("a")
	require.True(t, ok, "expected a hit within TTL")
	assert.Equal(t, 1, v)
}

func TestTTLLRU_MissAfterTTL(t *testing.T) {
	c := NewTTLLRU[string, int](3, time.Millisecond)
	c.Set("a", 1)

	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("a")
	assert.False(t, ok, "expected a miss after the entry's TTL elapsed")
	assert.Equal(t, 0, c.Len(), "expired entry should be lazily evicted on read")
}

func TestTTLLRU_MissWhenAbsent(t *testing.T) {
	c := NewTTLLRU[string, int](3, time.Hour)
	_, ok := c.Get("missing")
	assert.False(t, ok)
}

func TestTTLLRU_Eviction(t *testing.T) {
	// maxSize=2: insert a,b → full. Insert c → evicts LRU entry "a".
	c := NewTTLLRU[string, int](2, time.Hour)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	_, ok := c.Get("a")
	assert.False(t, ok, "a should have been evicted")

	v, ok := c.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)

	v, ok = c.Get("c")
	require.True(t, ok)
	assert.Equal(t, 3, v)

	assert.Equal(t, 2, c.Len())
}

func TestTTLLRU_SetWithTTLOverridesDefault(t *testing.T) {
	c := NewTTLLRU[string, int](3, time.Hour)
	c.SetWithTTL("short", 1, time.Millisecond)
	c.Set("long", 2) // uses the hour-long default

	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("short")
	assert.False(t, ok, "expected the entry-specific short TTL to have expired")

	v, ok := c.Get("long")
	require.True(t, ok, "expected the default-TTL entry to still be fresh")
	assert.Equal(t, 2, v)
}

// TestTTLLRU_SetWithTTLNonPositiveIsNoop proves the "never cache" contract:
// a ttl <= 0 must not store the entry at all, so every subsequent Get misses
// and every caller re-attempts — used by goanalysis.CachedLoadPackages to
// keep a caller-budget-specific failure (context.DeadlineExceeded) from
// poisoning a different caller's longer-budget attempt.
//
// RED guarantee: remove the `if ttl <= 0 { return }` guard in SetWithTTL and
// this test fails — the entry gets stored under a zero/negative TTL and Get
// either panics dividing by a bogus duration or (more likely) the
// time.Since(e.at) > e.ttl comparison with a non-positive ttl still evicts
// on read, which would coincidentally pass — so this test also checks Len()
// stays 0 immediately after Set, which only holds if Set genuinely skipped
// the insert.
func TestTTLLRU_SetWithTTLNonPositiveIsNoop(t *testing.T) {
	c := NewTTLLRU[string, int](3, time.Hour)
	c.SetWithTTL("never", 1, 0)

	assert.Equal(t, 0, c.Len(), "a ttl<=0 Set must not insert an entry at all")

	_, ok := c.Get("never")
	assert.False(t, ok)
}
