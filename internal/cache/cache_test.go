package cache

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ──────────────────────────────────────────────────────────────────
// ParseCache tests
// ──────────────────────────────────────────────────────────────────

func TestParseCacheGetSet(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go", Language: "go"}
	c.Put("/a.go", 1000, 200, false, result, nil)

	got, _ := c.Get("/a.go", 1000, 200, false)
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if got.File != "/a.go" {
		t.Fatalf("got File=%q, want /a.go", got.File)
	}
}

func TestParseCacheMiss(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	got, _ := c.Get("/missing.go", 1000, 200, false)
	if got != nil {
		t.Fatal("expected cache miss for missing key")
	}

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestParseCacheStaleModTime(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go"}
	c.Put("/a.go", 1000, 200, false, result, nil)

	// Different modTime → stale.
	got, _ := c.Get("/a.go", 2000, 200, false)
	if got != nil {
		t.Fatal("expected stale miss on modTime change")
	}
}

func TestParseCacheStaleSize(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	result := &parser.ParseResult{File: "/a.go"}
	c.Put("/a.go", 1000, 200, false, result, nil)

	// Different size → stale.
	got, _ := c.Get("/a.go", 1000, 300, false)
	if got != nil {
		t.Fatal("expected stale miss on size change")
	}
}

func TestParseCacheLRUEviction(t *testing.T) {
	t.Parallel()
	c := NewParseCache(3)

	c.Put("/a.go", 1, 1, false, &parser.ParseResult{File: "/a.go"}, nil)
	c.Put("/b.go", 2, 2, false, &parser.ParseResult{File: "/b.go"}, nil)
	c.Put("/c.go", 3, 3, false, &parser.ParseResult{File: "/c.go"}, nil)

	// Access /a.go to make it recent.
	c.Get("/a.go", 1, 1, false)

	// Add /d.go — should evict /b.go (LRU).
	c.Put("/d.go", 4, 4, false, &parser.ParseResult{File: "/d.go"}, nil)

	if got, _ := c.Get("/b.go", 2, 2, false); got != nil {
		t.Fatal("expected /b.go to be evicted")
	}
	if got, _ := c.Get("/a.go", 1, 1, false); got == nil {
		t.Fatal("expected /a.go to survive (recently accessed)")
	}
	if got, _ := c.Get("/d.go", 4, 4, false); got == nil {
		t.Fatal("expected /d.go to be present")
	}
}

func TestParseCacheUpdate(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	c.Put("/a.go", 1, 1, false, &parser.ParseResult{File: "/a.go", Language: "go"}, nil)
	c.Put("/a.go", 2, 2, false, &parser.ParseResult{File: "/a.go", Language: "python"}, nil)

	got, _ := c.Get("/a.go", 2, 2, false)
	if got == nil {
		t.Fatal("expected cache hit after update")
	}
	if got.Language != "python" {
		t.Fatalf("expected updated language=python, got %q", got.Language)
	}
}

func TestParseCacheStats(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	c.Put("/a.go", 1, 1, false, &parser.ParseResult{}, nil)
	c.Get("/a.go", 1, 1, false) // hit
	c.Get("/b.go", 2, 2, false) // miss

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
	t.Parallel()
	c := NewParseCache(100)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := "/file" + string(rune('A'+n%26)) + ".go"
			c.Put(path, int64(n), int64(n), false, &parser.ParseResult{File: path}, nil)
			c.Get(path, int64(n), int64(n), false)
		}(i)
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Entries < 1 {
		t.Fatal("expected at least 1 entry after concurrent access")
	}
}

// TestParseCacheIncludeBodyKeyed is a regression test for DEFECT 2: the
// cache key used to omit includeBody, so an entry cached with
// includeBody=false (AnalyzeRepo's mode) could be served to an
// includeBody=true request (AnalyzeForResearch's mode) sharing the same
// cache, silently returning a body-less result. Both modes must be keyed
// independently and coexist.
func TestParseCacheIncludeBodyKeyed(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	bodyFalse := &parser.ParseResult{File: "/a.go", Language: "go"}
	c.Put("/a.go", 1000, 200, false, bodyFalse, nil)

	// A includeBody=true request against a includeBody=false entry must miss.
	if got, _ := c.Get("/a.go", 1000, 200, true); got != nil {
		t.Fatal("expected miss for includeBody=true against a includeBody=false entry")
	}

	// The original includeBody=false entry is still retrievable.
	if got, _ := c.Get("/a.go", 1000, 200, false); got != bodyFalse {
		t.Fatal("expected includeBody=false entry to remain retrievable")
	}

	// Both modes coexist once both are cached.
	bodyTrue := &parser.ParseResult{File: "/a.go", Language: "go", Imports: []string{"marker"}}
	c.Put("/a.go", 1000, 200, true, bodyTrue, nil)

	if got, _ := c.Get("/a.go", 1000, 200, true); got != bodyTrue {
		t.Fatal("expected includeBody=true entry to be retrievable")
	}
	if got, _ := c.Get("/a.go", 1000, 200, false); got != bodyFalse {
		t.Fatal("expected includeBody=false entry to still be retrievable after includeBody=true Put")
	}
}

// TestParseCacheRoundTripsCalls is a regression test for DEFECT 1: Put used
// to store only the *parser.ParseResult and drop the call sites, so every
// cache HIT returned nil calls — silently emptying the PageRank call-graph
// on the second repo_analyze of an unchanged repo.
func TestParseCacheRoundTripsCalls(t *testing.T) {
	t.Parallel()
	c := NewParseCache(10)

	calls := []parser.CallSite{
		{Name: "Foo", Receiver: "pkg", Line: 3, File: "/a.go"},
		{Name: "Bar", Line: 7, File: "/a.go"},
	}
	c.Put("/a.go", 1000, 200, false, &parser.ParseResult{File: "/a.go"}, calls)

	_, gotCalls := c.Get("/a.go", 1000, 200, false)
	if len(gotCalls) != len(calls) {
		t.Fatalf("expected %d calls, got %d", len(calls), len(gotCalls))
	}
	if gotCalls[0] != calls[0] {
		t.Fatalf("got calls[0]=%+v, want %+v", gotCalls[0], calls[0])
	}
	if gotCalls[1] != calls[1] {
		t.Fatalf("got calls[1]=%+v, want %+v", gotCalls[1], calls[1])
	}
}

// ──────────────────────────────────────────────────────────────────
// LLMCache tests
// ──────────────────────────────────────────────────────────────────

func TestLLMCacheGetSet(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	c := NewLLMCache(10, time.Hour)

	_, ok := c.Get(12345)
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestLLMCacheTTLExpiry(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestLLMCacheTTLBoundary verifies freshness just before expiry and staleness just after.
//
// Runs inside a synctest bubble (Go 1.24+, stable in this repo's go 1.26
// toolchain) so time.Sleep/time.Now use a virtualized, deterministic clock
// instead of the wall clock. Previously this test slept real wall-clock
// milliseconds with a tight ±5ms margin around a 20ms TTL — under the
// self-hosted preflight runner's full-suite parallel load (the now-REQUIRED
// merge gate, see plan Phase 0a) scheduler jitter could push either sleep
// across the boundary and flip the assertion. NewLLMCache is constructed
// INSIDE the bubble so its background cleanup goroutine (kitcache's
// cleanupLoop) joins the bubble and is governed by the same fake clock —
// see testing/synctest package doc, "Any goroutines started within the
// bubble are also part of the bubble." The deferred c.c.Close() is required,
// not optional: synctest.Test panics ("deadlock: main bubble goroutine has
// exited but blocked goroutines remain") if cleanupLoop is still parked in
// its select when this closure returns, since a durably-blocked goroutine is
// not the same as an exited one. Zero production cache-logic change:
// go-kit/cache's TTL check (time.Now().After(e.expiresAt)) is exercised
// unmodified, just against virtual time.
func TestLLMCacheTTLBoundary(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const ttl = 20 * time.Millisecond
		c := NewLLMCache(10, ttl)
		defer c.c.Close()

		key := PromptHash("boundary", "test")
		c.Put(key, "value")

		// 5ms before expiry — must be fresh.
		time.Sleep(ttl - 5*time.Millisecond)
		if _, ok := c.Get(key); !ok {
			t.Error("expected cache hit just before TTL expiry")
		}

		// Re-put to reset, then sleep past TTL.
		c.Put(key, "value")
		time.Sleep(ttl + 5*time.Millisecond)
		if _, ok := c.Get(key); ok {
			t.Error("expected cache miss just after TTL expiry")
		}
	})
}

// TestLLMCacheTTLUpdateResetsExpiry verifies that re-putting a key resets its TTL.
//
// See TestLLMCacheTTLBoundary for why this runs inside a synctest bubble
// (same real-clock-under-load flake, same fix, same zero-production-change
// scope) and why the deferred c.c.Close() is required (unblocks
// kitcache's background cleanupLoop so it exits before the bubble closes).
func TestLLMCacheTTLUpdateResetsExpiry(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const ttl = 20 * time.Millisecond
		c := NewLLMCache(10, ttl)
		defer c.c.Close()

		key := PromptHash("reset", "expiry")
		c.Put(key, "v1")

		// Sleep half TTL, then re-put (resets timer).
		time.Sleep(ttl / 2)
		c.Put(key, "v2")

		// Sleep another half TTL — total elapsed ~TTL, but timer was reset at half-point.
		time.Sleep(ttl / 2)

		got, ok := c.Get(key)
		if !ok {
			t.Error("expected cache hit: re-put should have reset TTL")
		}
		if got != "v2" {
			t.Errorf("got %q, want %q", got, "v2")
		}
	})
}

// TestLLMCacheExpiredEvictsEntry verifies that a TTL miss removes the entry from stats.
func TestLLMCacheExpiredEvictsEntry(t *testing.T) {
	t.Parallel()
	c := NewLLMCache(10, time.Millisecond)

	c.Put(1, "x")
	if c.Stats().Entries != 1 {
		t.Fatal("expected 1 entry after Put")
	}

	time.Sleep(5 * time.Millisecond)

	// Get triggers lazy eviction.
	_, _ = c.Get(1)

	stats := c.Stats()
	if stats.Entries != 0 {
		t.Errorf("expired entry should be removed; got Entries=%d", stats.Entries)
	}
}

// TestLLMCacheZeroTTL verifies that TTL=0 falls back to the default (1h), not instant expiry.
func TestLLMCacheZeroTTL(t *testing.T) {
	t.Parallel()
	c := NewLLMCache(10, 0)

	c.Put(1, "hello")
	got, ok := c.Get(1)
	if !ok {
		t.Error("TTL=0 should use default (1h), entry must be fresh immediately after Put")
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

// TestLLMCacheNegativeTTL verifies that TTL=-1 falls back to the default (1h).
func TestLLMCacheNegativeTTL(t *testing.T) {
	t.Parallel()
	c := NewLLMCache(10, -1)

	c.Put(1, "hello")
	got, ok := c.Get(1)
	if !ok {
		t.Error("TTL=-1 should use default (1h), entry must be fresh immediately after Put")
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

// TestLLMCacheEvictionPrefersStalest fills the cache to capacity, lets half the entries
// expire, then adds a new entry. Verifies that the LRU entry is evicted (not necessarily
// the expired one — LLMCache uses LRU order, not staleness order) and the behavior
// is deterministic.
func TestLLMCacheEvictionPrefersStalest(t *testing.T) {
	t.Parallel()
	const ttl = 30 * time.Millisecond
	c := NewLLMCache(4, ttl)

	// Fill to capacity: keys 1–4.
	c.Put(1, "a")
	c.Put(2, "b")
	c.Put(3, "c")
	c.Put(4, "d")

	// Access keys 3 and 4 to make them recent; keys 1 and 2 are now LRU tail.
	_, _ = c.Get(3)
	_, _ = c.Get(4)

	// Let all entries age past half-TTL (not yet expired).
	time.Sleep(ttl / 2)

	// Add a 5th entry — LRU evicts key=1 (oldest in access order).
	c.Put(5, "e")

	if _, ok := c.Get(1); ok {
		t.Error("key=1 (LRU) should have been evicted on overflow")
	}
	if _, ok := c.Get(5); !ok {
		t.Error("newly added key=5 should be present")
	}

	// Verify entries count is still at capacity.
	stats := c.Stats()
	if stats.Entries != 4 {
		t.Errorf("expected 4 entries after eviction, got %d", stats.Entries)
	}
}

func TestPromptHash(t *testing.T) {
	t.Parallel()
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
