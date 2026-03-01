package cache

import (
	"context"
	"testing"
	"time"
)

func TestGenericCache_L1Only(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{MaxSize: 100, TTL: time.Hour})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "key1", "value1")

	got, ok := c.Get(ctx, "key1")
	if !ok {
		t.Fatal("expected cache hit for key1")
	}
	if got != "value1" {
		t.Fatalf("got %q, want %q", got, "value1")
	}

	_, ok = c.Get(ctx, "missing")
	if ok {
		t.Fatal("expected cache miss for missing key")
	}
}

func TestGenericCache_TTLExpiry(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{MaxSize: 100, TTL: 50 * time.Millisecond})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "key1", "value1")

	time.Sleep(60 * time.Millisecond)

	_, ok := c.Get(ctx, "key1")
	if ok {
		t.Fatal("expected TTL expiry for key1")
	}
}

func TestGenericCache_Stats(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{MaxSize: 100, TTL: time.Hour})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "a", "aval")
	c.Get(ctx, "a")       // hit
	c.Get(ctx, "missing") // miss

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry, got %d", stats.Entries)
	}
}

func TestGenericCache_LRUEviction(t *testing.T) {
	c := NewGenericCache[int](GenericCacheConfig{MaxSize: 3, TTL: time.Hour})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)
	c.Set(ctx, "c", 3)

	// Adding a 4th entry should evict.
	c.Set(ctx, "d", 4)

	s := c.Stats()
	if s.Entries > 3 {
		t.Fatalf("expected at most 3 entries, got %d", s.Entries)
	}

	got, ok := c.Get(ctx, "d")
	if !ok || got != 4 {
		t.Fatalf("expected 'd'=4, got ok=%v val=%v", ok, got)
	}
}

func TestGenericCache_Update(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{MaxSize: 10, TTL: time.Hour})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "k", "old")
	c.Set(ctx, "k", "new")

	got, ok := c.Get(ctx, "k")
	if !ok {
		t.Fatal("expected hit after update")
	}
	if got != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestGenericCache_Struct(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
	}

	c := NewGenericCache[payload](GenericCacheConfig{MaxSize: 10, TTL: time.Hour})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "p1", payload{Name: "alice", Score: 42})

	got, ok := c.Get(ctx, "p1")
	if !ok {
		t.Fatal("expected hit for struct payload")
	}
	if got.Name != "alice" || got.Score != 42 {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestCacheKey(t *testing.T) {
	k1 := Key("tool", "query1")
	k2 := Key("tool", "query2")
	k3 := Key("tool", "query1")

	if k1 == k2 {
		t.Fatal("different inputs should produce different keys")
	}
	if k1 != k3 {
		t.Fatal("same inputs should produce the same key")
	}
	if len(k1) == 0 {
		t.Fatal("key should be non-empty")
	}
}

func TestCacheKey_SinglePart(t *testing.T) {
	k := Key("only")
	if len(k) == 0 {
		t.Fatal("single-part key should be non-empty")
	}
}

func TestCacheKey_NullSeparation(t *testing.T) {
	// Key("a", "b") must differ from Key("ab") to avoid collisions.
	k1 := Key("a", "b")
	k2 := Key("ab")
	if k1 == k2 {
		t.Fatal("Key('a','b') must differ from Key('ab') due to null separator")
	}
}
