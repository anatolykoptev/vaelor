# Phase 5: go-search Migration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move code tools from go-search to go-code, add production infrastructure (retry, Redis cache, metrics, fallback LLM keys).

**Architecture:** Port code from go-search into new/existing internal packages in go-code. Add repo_search tool and extend repo_analyze with quick/issues modes. Delete migrated code from go-search.

**Tech Stack:** Go 1.24, go-redis/v9, go-stealth (retry), MCP SDK v1.4.0, GitHub API v3, SearXNG

---

### Task 1: internal/retry — Exponential Backoff

**Files:**
- Create: `internal/retry/retry.go`
- Create: `internal/retry/retry_test.go`

**Step 1: Write the failing test**

```go
// internal/retry/retry_test.go
package retry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryDo_Success(t *testing.T) {
	calls := 0
	result, err := Do(context.Background(), Options{MaxAttempts: 3, InitialDelay: time.Millisecond}, func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("got %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Fatalf("called %d times, want 1", calls)
	}
}

func TestRetryDo_RetriesThenSucceeds(t *testing.T) {
	calls := 0
	result, err := Do(context.Background(), Options{MaxAttempts: 3, InitialDelay: time.Millisecond}, func() (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("transient")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" || calls != 3 {
		t.Fatalf("got %q calls=%d, want ok/3", result, calls)
	}
}

func TestRetryDo_ExhaustedReturnsLastError(t *testing.T) {
	calls := 0
	_, err := Do(context.Background(), Options{MaxAttempts: 2, InitialDelay: time.Millisecond}, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("called %d times, want 2", calls)
	}
}

func TestRetryDo_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Do(ctx, Options{MaxAttempts: 5, InitialDelay: time.Millisecond}, func() (string, error) {
		return "", errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestRetryHTTP_Retries429(t *testing.T) {
	calls := 0
	resp, err := HTTP(context.Background(), Options{MaxAttempts: 3, InitialDelay: time.Millisecond}, func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("called %d times, want 3", calls)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/retry/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/retry/retry.go
package retry

import (
	"context"
	"net/http"
	"time"
)

const (
	DefaultMaxAttempts  = 3
	DefaultInitialDelay = 500 * time.Millisecond
	DefaultMaxDelay     = 5 * time.Second
)

// Options controls retry behavior.
type Options struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// defaults fills zero-valued fields with defaults.
func (o Options) defaults() Options {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.InitialDelay <= 0 {
		o.InitialDelay = DefaultInitialDelay
	}
	if o.MaxDelay <= 0 {
		o.MaxDelay = DefaultMaxDelay
	}
	return o
}

// Do retries fn up to MaxAttempts times with exponential backoff.
func Do[T any](ctx context.Context, opts Options, fn func() (T, error)) (T, error) {
	opts = opts.defaults()
	var lastErr error
	var zero T
	delay := opts.InitialDelay

	for attempt := range opts.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt < opts.MaxAttempts-1 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return zero, ctx.Err()
			}
			delay *= 2
			if delay > opts.MaxDelay {
				delay = opts.MaxDelay
			}
		}
	}
	return zero, lastErr
}

// retryableHTTPStatus returns true for HTTP status codes that should be retried.
func retryableHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// HTTP retries an HTTP request function, treating 429/5xx as retryable.
func HTTP(ctx context.Context, opts Options, fn func() (*http.Response, error)) (*http.Response, error) {
	return Do(ctx, opts, func() (*http.Response, error) {
		resp, err := fn()
		if err != nil {
			return nil, err
		}
		if retryableHTTPStatus(resp.StatusCode) {
			return resp, &HTTPError{StatusCode: resp.StatusCode}
		}
		return resp, nil
	})
}

// HTTPError is returned when an HTTP response has a retryable status code.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return http.StatusText(e.StatusCode)
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/retry/ -v`
Expected: PASS (5 tests)

**Step 5: Commit**

```bash
git add internal/retry/
git commit -m "feat: add internal/retry package with exponential backoff"
```

---

### Task 2: internal/metrics — Atomic Counters

**Files:**
- Create: `internal/metrics/metrics.go`
- Create: `internal/metrics/metrics_test.go`

**Step 1: Write the failing test**

```go
// internal/metrics/metrics_test.go
package metrics

import "testing"

func TestIncrAndGet(t *testing.T) {
	Reset()
	Incr(LLMCalls)
	Incr(LLMCalls)
	Incr(LLMErrors)

	m := Snapshot()
	if m[LLMCalls] != 2 {
		t.Fatalf("LLMCalls=%d, want 2", m[LLMCalls])
	}
	if m[LLMErrors] != 1 {
		t.Fatalf("LLMErrors=%d, want 1", m[LLMErrors])
	}
}

func TestTrackOperation(t *testing.T) {
	Reset()
	TrackOperation(LLMCalls, LLMErrors, func() error {
		return nil
	})
	m := Snapshot()
	if m[LLMCalls] != 1 {
		t.Fatalf("LLMCalls=%d, want 1", m[LLMCalls])
	}
	if m[LLMErrors] != 0 {
		t.Fatalf("LLMErrors=%d, want 0", m[LLMErrors])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/metrics/ -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/metrics/metrics.go
package metrics

import "sync/atomic"

// Counter names.
const (
	LLMCalls        = "llm_calls"
	LLMErrors       = "llm_errors"
	SearchRequests  = "search_requests"
	GitClones       = "git_clones"
	CacheHits       = "cache_hits"
	CacheMisses     = "cache_misses"
	GitHubAPICalls  = "github_api_calls"
)

var counters sync.Map // map[string]*atomic.Int64

func counter(name string) *atomic.Int64 {
	v, _ := counters.LoadOrStore(name, &atomic.Int64{})
	return v.(*atomic.Int64)
}

// Incr increments a counter by 1.
func Incr(name string) { counter(name).Add(1) }

// Snapshot returns a copy of all counters.
func Snapshot() map[string]int64 {
	m := make(map[string]int64)
	counters.Range(func(key, val any) bool {
		m[key.(string)] = val.(*atomic.Int64).Load()
		return true
	})
	return m
}

// Reset clears all counters (for tests).
func Reset() {
	counters.Range(func(key, _ any) bool {
		counters.Delete(key)
		return true
	})
}

// TrackOperation increments callCounter, runs fn, increments errCounter on error.
func TrackOperation(callCounter, errCounter string, fn func() error) error {
	Incr(callCounter)
	if err := fn(); err != nil {
		Incr(errCounter)
		return err
	}
	return nil
}
```

Note: needs `import "sync"` — add `sync` to imports alongside `sync/atomic`.

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/metrics/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/metrics/
git commit -m "feat: add internal/metrics package with atomic counters"
```

---

### Task 3: internal/cache — Add Redis L2

**Files:**
- Modify: `internal/cache/cache.go`
- Create: `internal/cache/redis.go`
- Create: `internal/cache/redis_test.go`
- Modify: `go.mod` (add `github.com/redis/go-redis/v9`)

**Step 1: Add go-redis dependency**

Run: `cd /path/to/repos/src/go-code && go get github.com/redis/go-redis/v9`

**Step 2: Write the failing test**

```go
// internal/cache/redis_test.go
package cache

import (
	"context"
	"testing"
	"time"
)

func TestGenericCache_L1Only(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{
		MaxSize: 100,
		TTL:     time.Hour,
	})

	ctx := context.Background()
	c.Set(ctx, "key1", "value1")
	got, ok := c.Get(ctx, "key1")
	if !ok || got != "value1" {
		t.Fatalf("Get=%q/%v, want value1/true", got, ok)
	}

	_, ok = c.Get(ctx, "missing")
	if ok {
		t.Fatal("expected miss for missing key")
	}
}

func TestGenericCache_TTLExpiry(t *testing.T) {
	c := NewGenericCache[string](GenericCacheConfig{
		MaxSize: 100,
		TTL:     50 * time.Millisecond,
	})

	ctx := context.Background()
	c.Set(ctx, "key1", "value1")
	time.Sleep(60 * time.Millisecond)

	_, ok := c.Get(ctx, "key1")
	if ok {
		t.Fatal("expected miss after TTL expiry")
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
		t.Fatal("same inputs should produce same key")
	}
	if len(k1) != 16 {
		t.Fatalf("key length=%d, want 16 (hex-encoded 8 bytes)", len(k1))
	}
}
```

**Step 3: Write implementation**

```go
// internal/cache/redis.go
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key generates a deterministic cache key from parts using SHA256.
func Key(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// GenericCacheConfig configures a GenericCache.
type GenericCacheConfig struct {
	MaxSize  int
	TTL      time.Duration
	RedisURL string // optional — L2 cache
}

// GenericCache is a tiered (L1 in-memory + optional L2 Redis) cache for any JSON-serializable type.
type GenericCache[T any] struct {
	mu      sync.Mutex
	l1      map[string]*genericEntry[T]
	maxSize int
	ttl     time.Duration
	rdb     *redis.Client
	hits    int64
	misses  int64
}

type genericEntry[T any] struct {
	Value     T
	ExpiresAt time.Time
}

// NewGenericCache creates a tiered cache. If RedisURL is provided, Redis is used as L2.
func NewGenericCache[T any](cfg GenericCacheConfig) *GenericCache[T] {
	c := &GenericCache[T]{
		l1:      make(map[string]*genericEntry[T], cfg.MaxSize),
		maxSize: cfg.MaxSize,
		ttl:     cfg.TTL,
	}
	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err == nil {
			rdb := redis.NewClient(opts)
			if rdb.Ping(context.Background()).Err() == nil {
				c.rdb = rdb
				slog.Info("cache: Redis L2 connected", slog.String("url", cfg.RedisURL))
			} else {
				slog.Warn("cache: Redis L2 unreachable, using L1 only", slog.String("url", cfg.RedisURL))
			}
		}
	}
	return c
}

// Get retrieves a value. Checks L1 first, then L2 (Redis). Promotes L2 hits to L1.
func (c *GenericCache[T]) Get(ctx context.Context, key string) (T, bool) {
	c.mu.Lock()
	if e, ok := c.l1[key]; ok {
		if time.Now().Before(e.ExpiresAt) {
			c.hits++
			c.mu.Unlock()
			return e.Value, true
		}
		delete(c.l1, key)
	}
	c.mu.Unlock()

	// L2: Redis
	if c.rdb != nil {
		data, err := c.rdb.Get(ctx, key).Bytes()
		if err == nil {
			var val T
			if json.Unmarshal(data, &val) == nil {
				c.promote(key, val)
				c.mu.Lock()
				c.hits++
				c.mu.Unlock()
				return val, true
			}
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
	c.evictIfNeeded()
	c.l1[key] = &genericEntry[T]{Value: val, ExpiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()

	if c.rdb != nil {
		data, err := json.Marshal(val)
		if err == nil {
			c.rdb.Set(ctx, key, data, c.ttl)
		}
	}
}

func (c *GenericCache[T]) promote(key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIfNeeded()
	c.l1[key] = &genericEntry[T]{Value: val, ExpiresAt: time.Now().Add(c.ttl)}
}

func (c *GenericCache[T]) evictIfNeeded() {
	if len(c.l1) < c.maxSize {
		return
	}
	// Evict oldest entry.
	var oldestKey string
	var oldestTime time.Time
	for k, e := range c.l1 {
		if oldestKey == "" || e.ExpiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.ExpiresAt
		}
	}
	if oldestKey != "" {
		delete(c.l1, oldestKey)
	}
}

// Stats returns L1 cache statistics.
func (c *GenericCache[T]) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Hits: c.hits, Misses: c.misses, Entries: len(c.l1)}
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/cache/ -v`
Expected: PASS (existing + new tests)

**Step 5: Commit**

```bash
git add internal/cache/ go.mod go.sum
git commit -m "feat: add GenericCache with Redis L2 support to internal/cache"
```

---

### Task 4: internal/llm — Retry + Fallback Keys

**Files:**
- Modify: `internal/llm/llm.go`
- Create: `internal/llm/llm_test.go`

**Step 1: Write the failing test**

```go
// internal/llm/llm_test.go
package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestComplete_RetriesOn500(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "test", MaxRetries: 3})
	result, err := c.Complete(context.Background(), "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("got %q, want %q", result, "ok")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d, want 3", calls.Load())
	}
}

func TestComplete_FallbackKey(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		key := r.Header.Get("Authorization")
		if n <= 2 && key == "Bearer primary" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"fallback-ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:      srv.URL,
		APIKey:       "primary",
		FallbackKeys: []string{"secondary"},
		MaxRetries:   2,
	})
	result, err := c.Complete(context.Background(), "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if result != "fallback-ok" {
		t.Fatalf("got %q, want %q", result, "fallback-ok")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/llm/ -v -run TestComplete`
Expected: FAIL (Config struct missing FallbackKeys, MaxRetries)

**Step 3: Modify llm.go**

Add to `Config`:
```go
// FallbackKeys are tried if the primary APIKey gets 429/5xx.
FallbackKeys []string

// MaxRetries is the max retry attempts per key. Default: 2.
MaxRetries int
```

Add to `Client`:
```go
fallbackKeys []string
maxRetries   int
```

Update `NewClient` to store fallback keys and maxRetries.

Modify `Complete` to:
1. Try primary key with retry (maxRetries attempts)
2. On exhaustion, try each fallback key with retry
3. Use `internal/retry.Do` for the retry loop

Add convenience method:
```go
// CompleteRaw sends a single user prompt (no system prompt).
func (c *Client) CompleteRaw(ctx context.Context, prompt string) (string, error) {
	return c.Complete(ctx, "", prompt)
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/llm/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/
git commit -m "feat: add retry and fallback API keys to LLM client"
```

---

### Task 5: internal/github — Extend with Search APIs

**Files:**
- Modify: `internal/github/github.go`
- Create: `internal/github/search.go`
- Create: `internal/github/search_test.go`

**Step 1: Write the failing test**

```go
// internal/github/search_test.go
package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/foo/bar", "foo", "bar", true},
		{"https://github.com/foo/bar.git", "foo", "bar", true},
		{"https://github.com/topics/golang", "", "", false},
		{"https://example.com/foo/bar", "", "", false},
	}
	for _, tt := range tests {
		owner, repo, ok := ExtractOwnerRepo(tt.url)
		if owner != tt.owner || repo != tt.repo || ok != tt.ok {
			t.Errorf("ExtractOwnerRepo(%q) = %q,%q,%v; want %q,%q,%v",
				tt.url, owner, repo, ok, tt.owner, tt.repo, tt.ok)
		}
	}
}

func TestSearchCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"name":"main.go","path":"cmd/main.go",
			"html_url":"https://github.com/foo/bar/blob/main/cmd/main.go",
			"repository":{"full_name":"foo/bar"},
			"text_matches":[{"fragment":"func main()"}]
		}]}`))
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL
	results, err := c.SearchCode(context.Background(), "main", []string{"foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Name != "main.go" {
		t.Fatalf("got name=%q, want main.go", results[0].Name)
	}
}

func TestSearchIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"number":42,"title":"Fix bug","html_url":"https://github.com/foo/bar/issues/42",
			"state":"open","user":{"login":"alice"},"body":"desc","comments":3,
			"created_at":"2026-01-01","labels":[{"name":"bug"}],
			"repository":{"full_name":"foo/bar"}
		}]}`))
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL
	issues, err := c.SearchIssues(context.Background(), "is:issue repo:foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].Number != 42 {
		t.Fatalf("got number=%d, want 42", issues[0].Number)
	}
}

func TestSearchRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"full_name":"foo/bar","description":"A library",
			"stargazers_count":100,"language":"Go","html_url":"https://github.com/foo/bar",
			"topics":["go"],"pushed_at":"2026-01-01"
		}]}`))
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL
	results, err := c.SearchRepos(context.Background(), "go library", "stars")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].FullName != "foo/bar" {
		t.Fatalf("got %q, want foo/bar", results[0].FullName)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/github/ -v`
Expected: FAIL

**Step 3: Write search.go**

Port `SearchGitHubCode`, `SearchGitHubIssues`, `SearchGitHubRepos`, `ExtractOwnerRepo` from go-search's `sources/github.go`. Adapt to use `Client` struct instead of global `engine.Cfg`. Define return types:

```go
// internal/github/search.go

// CodeResult represents a GitHub Code Search result.
type CodeResult struct {
	Name    string
	Path    string
	URL     string
	Repo    string
	Content string // joined text-match fragments
}

// IssueItem represents a GitHub issue or PR from search.
type IssueItem struct {
	Number    int
	Title     string
	URL       string
	State     string
	Author    string
	Labels    []string
	Body      string
	Comments  int
	CreatedAt string
	MergedAt  string
	Repo      string
}

// RepoSearchResult represents a repo from GitHub Search API.
type RepoSearchResult struct {
	FullName    string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	HTMLURL     string
}
```

Also make `apiBase` a field on `Client` (currently a const). Change:
```go
type Client struct {
	http    *http.Client
	token   string
	apiBase string  // NEW: defaults to "https://api.github.com", overridable for tests
}
```

Update `NewClient` to set `apiBase` to the const. Update `FetchRepoMeta` and `FetchREADME` to use `c.apiBase`.

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/github/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat: add SearchCode, SearchIssues, SearchRepos to github package"
```

---

### Task 6: internal/search — SearXNG Client

**Files:**
- Create: `internal/search/searxng.go`
- Create: `internal/search/types.go`
- Create: `internal/search/searxng_test.go`

**Step 1: Write the failing test**

```go
// internal/search/searxng_test.go
package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchSearXNG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			t.Fatal("missing q parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"Test","url":"https://example.com","content":"desc","score":0.9}]}`))
	}))
	defer srv.Close()

	c := NewSearXNGClient(srv.URL)
	results, err := c.Search(context.Background(), "test", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "Test" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestFilterByScore(t *testing.T) {
	results := []Result{
		{Title: "A", Score: 0.9},
		{Title: "B", Score: 0.1},
		{Title: "C", Score: 0.5},
	}
	filtered := FilterByScore(results, 0.5, 1)
	if len(filtered) != 2 {
		t.Fatalf("got %d results, want 2", len(filtered))
	}
}

func TestDedupByDomain(t *testing.T) {
	results := []Result{
		{URL: "https://example.com/a"},
		{URL: "https://example.com/b"},
		{URL: "https://example.com/c"},
		{URL: "https://other.com/a"},
	}
	deduped := DedupByDomain(results, 2)
	if len(deduped) != 3 {
		t.Fatalf("got %d results, want 3 (2 from example + 1 from other)", len(deduped))
	}
}
```

**Step 2: Run test, verify fail**

**Step 3: Write implementation**

```go
// internal/search/types.go
package search

// Result is a search result from SearXNG or other sources.
type Result struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// SearchOpts controls SearXNG search behavior.
type SearchOpts struct {
	Language  string
	TimeRange string
	Engines   string
}
```

```go
// internal/search/searxng.go
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SearXNGClient queries a SearXNG instance.
type SearXNGClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSearXNGClient creates a SearXNG client.
func NewSearXNGClient(baseURL string) *SearXNGClient {
	return &SearXNGClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

type searxngResponse struct {
	Results []Result `json:"results"`
}

// Search queries SearXNG and returns results.
func (c *SearXNGClient) Search(ctx context.Context, query string, opts SearchOpts) ([]Result, error) {
	u, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("parse searxng url: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	if opts.Language != "" && opts.Language != "all" {
		q.Set("language", opts.Language)
	}
	if opts.TimeRange != "" {
		q.Set("time_range", opts.TimeRange)
	}
	if opts.Engines != "" {
		q.Set("engines", opts.Engines)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned %d", resp.StatusCode)
	}

	var data searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Results, nil
}

// FilterByScore removes results below minScore, keeping at least minKeep.
func FilterByScore(results []Result, minScore float64, minKeep int) []Result {
	var out []Result
	for _, r := range results {
		if r.Score >= minScore {
			out = append(out, r)
		}
	}
	if len(out) < minKeep && len(results) >= minKeep {
		return results[:minKeep]
	}
	if len(out) < minKeep {
		return results
	}
	return out
}

// DedupByDomain limits results to maxPerDomain per domain.
func DedupByDomain(results []Result, maxPerDomain int) []Result {
	counts := make(map[string]int)
	var out []Result
	for _, r := range results {
		u, err := url.Parse(r.URL)
		if err != nil {
			out = append(out, r)
			continue
		}
		domain := u.Host
		if counts[domain] < maxPerDomain {
			out = append(out, r)
			counts[domain]++
		}
	}
	return out
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/search/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/search/
git commit -m "feat: add internal/search package with SearXNG client"
```

---

### Task 7: Extend Config + Register New Dependencies

**Files:**
- Modify: `cmd/go-code/config.go`
- Modify: `cmd/go-code/register.go`
- Modify: `cmd/go-code/main.go`
- Modify: `internal/analyze/analyze.go` (Deps struct)

**Step 1: Add new config fields**

Add to `Config` struct in `config.go`:
```go
SearxngURL       string
RedisURL         string
LLMFallbackKeys []string
GithubSearchRepos []string
```

Add to `loadConfig()`:
```go
SearxngURL:       env("SEARXNG_URL", "http://searxng:8888"),
RedisURL:         env("REDIS_URL", ""),
LLMFallbackKeys: envList("LLM_API_KEY_FALLBACK", ""),
GithubSearchRepos: envList("GITHUB_SEARCH_REPOS", ""),
```

Add `envList` helper:
```go
func envList(key, def string) []string {
	v := env(key, def)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
```

**Step 2: Update Deps with new dependencies**

Add to `analyze.Deps`:
```go
SearXNG    *search.SearXNGClient
ToolCache  *cache.GenericCache[string] // generic cache for tool results
```

**Step 3: Update register.go**

Initialize SearXNG client, GenericCache, pass FallbackKeys to LLM Config.

**Step 4: Update main.go**

Change `toolCount = 8` (was 6). Add health endpoint info about SearXNG/Redis connectivity.

**Step 5: Run `go build`**

Run: `cd /path/to/repos/src/go-code && go build ./cmd/go-code/`
Expected: builds without errors

**Step 6: Commit**

```bash
git add cmd/go-code/ internal/analyze/analyze.go
git commit -m "feat: extend config with SearXNG, Redis, fallback keys"
```

---

### Task 8: tool_repo_analyze — Quick + Issues Modes

**Files:**
- Modify: `cmd/go-code/tool_repo_analyze.go`

**Step 1: Extend RepoAnalyzeInput**

Add fields to existing struct:
```go
Type    string   `json:"type,omitempty" jsonschema_description:"Search type: pr (pull requests) or issue (GitHub issues). Switches to GitHub Issues Search API."`
Repos   []string `json:"repos,omitempty" jsonschema_description:"Multiple repos for quick mode (e.g. ['owner/repo1','owner/repo2'])"`
Pattern string   `json:"pattern,omitempty" jsonschema_description:"File include pattern for filtering"`
```

**Step 2: Add routing in handler**

Add before existing mode routing:
```go
// Issues/PRs mode.
if input.Type == "pr" || input.Type == "issue" {
    return handleIssuesMode(ctx, input, deps)
}
// Quick mode.
if input.Mode == "quick" || input.Mode == "raw" {
    return handleQuickMode(ctx, input, deps)
}
```

**Step 3: Implement handleQuickMode**

Port logic from go-search's `handleQuickMode`. Use `deps.GitHub.SearchCode()` instead of `sources.SearchGitHubCode()`. Use `deps.LLM.CompleteRaw()` for summarization with `SystemPromptQuickSearch` prompt.

**Step 4: Implement handleIssuesMode**

Port logic from go-search's `handleIssuesMode`. Use `deps.GitHub.SearchIssues()`. Use `deps.LLM.CompleteRaw()` for summarization.

**Step 5: Add new system prompts**

Add to `internal/llm/llm.go`:
```go
const SystemPromptQuickSearch = `You are analyzing GitHub code search results. Summarize the relevant code patterns found for the query. Be concise, reference file paths.`

const SystemPromptIssuesAnalysis = `You are analyzing GitHub issues/PRs. Summarize the key findings for the query. Focus on what's most relevant. Be concise.`
```

**Step 6: Run build + test**

Run: `cd /path/to/repos/src/go-code && go build ./cmd/go-code/ && go test ./... -count=1`
Expected: builds and all tests pass

**Step 7: Commit**

```bash
git add cmd/go-code/ internal/llm/
git commit -m "feat: add quick and issues/PRs modes to repo_analyze"
```

---

### Task 9: tool_repo_search — New Tool

**Files:**
- Create: `cmd/go-code/tool_repo_search.go`

**Step 1: Write the tool**

Port logic from go-search's `tool_github_repo_search.go`. Key differences:
- Use `deps.SearXNG.Search()` instead of `engine.SearchSearXNG()`
- Use `deps.GitHub.SearchRepos()` instead of `sources.SearchGitHubRepos()`
- Use `deps.GitHub.FetchRepoMeta()` and `deps.GitHub.FetchREADME()` for enrichment
- Use `deps.LLM.CompleteRaw()` for summarization
- Use `deps.ToolCache` for caching
- Use `cache.Key()` for cache key generation

```go
type RepoSearchInput struct {
	Query    string `json:"query" jsonschema_description:"What repositories to find. Supports GitHub syntax: 'language:go topic:ai', 'stars:>100'"`
	Language string `json:"language,omitempty" jsonschema_description:"Filter by programming language"`
	Sort     string `json:"sort,omitempty" jsonschema_description:"Sort by: stars, forks, updated"`
}
```

**Step 2: Register in register.go**

Add `registerRepoSearch(server, cfg, deps)` call.

**Step 3: Run build**

Run: `cd /path/to/repos/src/go-code && go build ./cmd/go-code/`
Expected: builds

**Step 4: Commit**

```bash
git add cmd/go-code/tool_repo_search.go cmd/go-code/register.go
git commit -m "feat: add repo_search tool (migrated from go-search)"
```

---

### Task 10: Deploy + Verify go-code

**Files:**
- Modify: `deploy/go-code.env` (add new env vars)
- Modify: `Dockerfile` if needed (add redis dependency)

**Step 1: Update docker-compose env**

Add to `~/deploy/example-server/.env`:
```
GO_CODE_SEARXNG_URL=http://searxng:8888
GO_CODE_REDIS_URL=redis://redis:6379/6
```

**Step 2: Build and deploy**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Verify via MCP**

Test each new mode:
- `repo_analyze` with `mode=quick` + `repo=anthropics/claude-code` + `query="MCP server"`
- `repo_analyze` with `type=issue` + `repo=anthropics/claude-code` + `query="bug"`
- `repo_search` with `query="golang MCP server framework"`

**Step 4: Commit env changes if needed**

---

### Task 11: Remove Code from go-search

**Files (in ~/src/go-search/):**
- Delete: `tool_github_repo_analyze.go`
- Delete: `tool_github_repo_search.go`
- Delete: `internal/gitingest/` (entire directory)
- Modify: `internal/engine/sources/github.go` (remove SearchGitHubCode, SearchGitHubIssues, SearchGitHubRepos, FetchRepoMeta, FetchREADME, ExtractOwnerRepo)
- Modify: `register.go` (remove 2 registerGithub* calls)
- Modify: `main.go` (update tool count: 12 → 10)
- Modify: `CLAUDE.md` (remove github_repo_analyze and github_repo_search tool entries)
- Modify: `internal/engine/metrics.go` (remove GitingestRequests counter)

**Step 1: Delete files**

```bash
cd ~/src/go-search
rm tool_github_repo_analyze.go tool_github_repo_search.go
rm -rf internal/gitingest/
```

**Step 2: Clean up register.go**

Remove `registerGithubRepoAnalyze(server)` and `registerGithubRepoSearch(server)` calls.

**Step 3: Clean up github.go**

Remove: SearchGitHubCode, SearchGitHubIssues, SearchGitHubRepos, FetchRepoMeta, FetchREADME, ExtractOwnerRepo, and related private types/helpers. Keep only functions still used by other go-search tools (check `GithubRawURL` etc.).

**Step 4: Update main.go tool count**

**Step 5: Build and test go-search**

```bash
cd ~/src/go-search && go build . && go test ./... -count=1
```

**Step 6: Deploy go-search**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-search
docker compose up -d --no-deps --force-recreate go-search
```

**Step 7: Commit**

```bash
cd ~/src/go-search
git add -A
git commit -m "feat: remove code tools migrated to go-code (Phase 5)"
```

---

### Task 12: Update Documentation

**Files:**
- Modify: `~/src/go-code/CLAUDE.md`
- Modify: `~/src/go-code/docs/ROADMAP.md`
- Modify: `~/src/go-search/CLAUDE.md`
- Modify: `~/.claude/projects/-home-example/memory/MEMORY.md`

**Step 1: Update go-code CLAUDE.md**

- Add `repo_search` to tool table
- Add `mode=quick`, `type=pr|issue` to `repo_analyze` description
- Update tool count: 6 → 8
- Add new env vars section
- Update architecture diagram with new packages

**Step 2: Update go-code ROADMAP.md**

Mark Phase 5 tasks as complete. Add v1.7.0 release entry.

**Step 3: Update go-search CLAUDE.md**

Remove `github_repo_analyze` and `github_repo_search` from tool table. Remove `internal/gitingest/` from architecture. Update tool count: 12 → 10.

**Step 4: Update MEMORY.md**

Update go-code section: mention 8 tools, new packages, Phase 5 complete.
Update go-search section: mention 10 tools, code tools removed.

**Step 5: Commit both repos**

```bash
cd ~/src/go-code && git add CLAUDE.md docs/ROADMAP.md && git commit -m "docs: update for Phase 5 migration"
cd ~/src/go-search && git add CLAUDE.md && git commit -m "docs: update after Phase 5 code tool migration"
```

---

### Task 13: Tag Release + Clean Up Worktrees

**Step 1: Tag go-code v1.7.0**

```bash
cd ~/src/go-code
git tag v1.7.0
git push origin v1.7.0
```

**Step 2: Clean up leftover worktrees**

```bash
cd ~/src/go-code
rm -rf .claude/worktrees/agent-*
```

**Step 3: Verify everything works**

- Test `repo_analyze` (all modes: deep, local, quick, issues)
- Test `repo_search`
- Test `code_compare`, `dep_graph`, `symbol_search`, `call_trace` (no regressions)
- Test go-search tools still work (smart_search, etc.)
