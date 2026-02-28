package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ──────────────────────────────────────────────────────────────────
// ParseCache tests
// ──────────────────────────────────────────────────────────────────

func TestParseCacheGetSet(t *testing.T) {
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go", Language: "go"}
	c.Put("/a.go", 1000, 200, result)

	got := c.Get("/a.go", 1000, 200)
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if got.File != "/a.go" {
		t.Fatalf("got File=%q, want /a.go", got.File)
	}
}

func TestParseCacheMiss(t *testing.T) {
	c := NewParseCache(10)

	got := c.Get("/missing.go", 1000, 200)
	if got != nil {
		t.Fatal("expected cache miss for missing key")
	}

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestParseCacheStaleModTime(t *testing.T) {
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go"}
	c.Put("/a.go", 1000, 200, result)

	// Different modTime → stale.
	got := c.Get("/a.go", 2000, 200)
	if got != nil {
		t.Fatal("expected stale miss on modTime change")
	}
}

func TestParseCacheStaleSize(t *testing.T) {
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go"}
	c.Put("/a.go", 1000, 200, result)

	// Different size → stale.
	got := c.Get("/a.go", 1000, 300)
	if got != nil {
		t.Fatal("expected stale miss on size change")
	}
}

func TestParseCacheLRUEviction(t *testing.T) {
	c := NewParseCache(3)

	c.Put("/a.go", 1, 1, &parser.ParseResult{File: "/a.go"})
	c.Put("/b.go", 2, 2, &parser.ParseResult{File: "/b.go"})
	c.Put("/c.go", 3, 3, &parser.ParseResult{File: "/c.go"})

	// Access /a.go to make it recent.
	c.Get("/a.go", 1, 1)

	// Add /d.go — should evict /b.go (LRU).
	c.Put("/d.go", 4, 4, &parser.ParseResult{File: "/d.go"})

	if c.Get("/b.go", 2, 2) != nil {
		t.Fatal("expected /b.go to be evicted")
	}
	if c.Get("/a.go", 1, 1) == nil {
		t.Fatal("expected /a.go to survive (recently accessed)")
	}
	if c.Get("/d.go", 4, 4) == nil {
		t.Fatal("expected /d.go to be present")
	}
}

func TestParseCacheUpdate(t *testing.T) {
	c := NewParseCache(10)

	c.Put("/a.go", 1, 1, &parser.ParseResult{File: "/a.go", Language: "go"})
	c.Put("/a.go", 2, 2, &parser.ParseResult{File: "/a.go", Language: "python"})

	got := c.Get("/a.go", 2, 2)
	if got == nil {
		t.Fatal("expected cache hit after update")
	}
	if got.Language != "python" {
		t.Fatalf("expected updated language=python, got %q", got.Language)
	}
}

func TestParseCacheStats(t *testing.T) {
	c := NewParseCache(10)

	c.Put("/a.go", 1, 1, &parser.ParseResult{})
	c.Get("/a.go", 1, 1) // hit
	c.Get("/b.go", 2, 2) // miss

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

func TestParseCacheConcurrent(t *testing.T) {
	c := NewParseCache(100)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := "/file" + string(rune('A'+n%26)) + ".go"
			c.Put(path, int64(n), int64(n), &parser.ParseResult{File: path})
			c.Get(path, int64(n), int64(n))
		}(i)
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Entries < 1 {
		t.Fatal("expected at least 1 entry after concurrent access")
	}
}

// ──────────────────────────────────────────────────────────────────
// LLMCache tests
// ──────────────────────────────────────────────────────────────────

func TestLLMCacheGetSet(t *testing.T) {
	c := NewLLMCache(10, time.Hour)

	key := PromptHash("system", "user")
	c.Put(key, "response text")

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "response text" {
		t.Fatalf("got %q, want %q", got, "response text")
	}
}

func TestLLMCacheMiss(t *testing.T) {
	c := NewLLMCache(10, time.Hour)

	_, ok := c.Get(12345)
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestLLMCacheTTLExpiry(t *testing.T) {
	// 1ms TTL for fast expiry.
	c := NewLLMCache(10, time.Millisecond)

	key := PromptHash("sys", "usr")
	c.Put(key, "answer")

	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected TTL expiry")
	}
}

func TestLLMCacheLRUEviction(t *testing.T) {
	c := NewLLMCache(3, time.Hour)

	c.Put(1, "a")
	c.Put(2, "b")
	c.Put(3, "c")

	// Access key=1 to make it recent.
	c.Get(1)

	// Add key=4 → should evict key=2 (LRU).
	c.Put(4, "d")

	if _, ok := c.Get(2); ok {
		t.Fatal("expected key=2 to be evicted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("expected key=1 to survive")
	}
}

func TestLLMCacheUpdate(t *testing.T) {
	c := NewLLMCache(10, time.Hour)

	c.Put(1, "old")
	c.Put(1, "new")

	got, ok := c.Get(1)
	if !ok {
		t.Fatal("expected hit after update")
	}
	if got != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestLLMCacheStats(t *testing.T) {
	c := NewLLMCache(10, time.Hour)

	c.Put(1, "a")
	c.Get(1) // hit
	c.Get(2) // miss

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestLLMCacheConcurrent(t *testing.T) {
	c := NewLLMCache(100, time.Hour)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := uint64(n % 30) //nolint:mnd // intentional collision for stress test
			c.Put(key, "response")
			c.Get(key)
		}(i)
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Entries < 1 {
		t.Fatal("expected at least 1 entry after concurrent access")
	}
}

func TestPromptHash(t *testing.T) {
	h1 := PromptHash("system", "user")
	h2 := PromptHash("system", "user")
	h3 := PromptHash("system", "different")

	if h1 != h2 {
		t.Fatal("same inputs should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different inputs should produce different hash")
	}
}
