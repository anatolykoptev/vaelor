package freshness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// memCache is an in-memory Cache implementation for testing.
type memCache struct {
	mu    sync.Mutex
	store map[string]string
}

func newMemCache() *memCache {
	return &memCache{store: make(map[string]string)}
}

func (m *memCache) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[key]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}

func (m *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}

// fakeRegistry returns a fixed version and counts calls.
type fakeRegistry struct {
	mu      sync.Mutex
	version string
	calls   int
}

func (f *fakeRegistry) Latest(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.version, nil
}

func (f *fakeRegistry) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// errCache always returns errors.
type errCache struct{}

func (errCache) Get(context.Context, string) (string, error) {
	return "", errors.New("cache down")
}

func (errCache) Set(context.Context, string, string, time.Duration) error {
	return errors.New("cache down")
}

func TestCachedRegistry_HitSkipsInner(t *testing.T) {
	inner := &fakeRegistry{version: "2.0.0"}
	cache := newMemCache()
	cached := NewCachedRegistry(inner, cache, "go", DefaultCacheTTL)

	ctx := context.Background()

	// First call — cache miss, hits inner.
	v, err := cached.Latest(ctx, "example.com/pkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "2.0.0" {
		t.Fatalf("got %q, want %q", v, "2.0.0")
	}
	if inner.callCount() != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.callCount())
	}

	// Second call — cache hit, inner NOT called.
	v, err = cached.Latest(ctx, "example.com/pkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "2.0.0" {
		t.Fatalf("got %q, want %q", v, "2.0.0")
	}
	if inner.callCount() != 1 {
		t.Fatalf("inner calls = %d, want 1 (should be cached)", inner.callCount())
	}
}

func TestCachedRegistry_CacheKeyFormat(t *testing.T) {
	inner := &fakeRegistry{version: "1.0.0"}
	cache := newMemCache()
	cached := NewCachedRegistry(inner, cache, "npm", DefaultCacheTTL)

	ctx := context.Background()
	_, _ = cached.Latest(ctx, "express")

	wantKey := "dep:fresh:npm:express"
	if _, ok := cache.store[wantKey]; !ok {
		t.Fatalf("expected cache key %q, got keys: %v", wantKey, cache.store)
	}
}

func TestCachedRegistry_FallbackOnCacheError(t *testing.T) {
	inner := &fakeRegistry{version: "3.0.0"}
	cached := NewCachedRegistry(inner, errCache{}, "rust", DefaultCacheTTL)

	ctx := context.Background()
	v, err := cached.Latest(ctx, "serde")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "3.0.0" {
		t.Fatalf("got %q, want %q", v, "3.0.0")
	}
	// Should still work on second call (cache always errors).
	v, err = cached.Latest(ctx, "serde")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "3.0.0" {
		t.Fatalf("got %q, want %q", v, "3.0.0")
	}
	if inner.callCount() != 2 {
		t.Fatalf("inner calls = %d, want 2 (cache broken)", inner.callCount())
	}
}

func TestNewMultiRegistryWithCache_NilCache(t *testing.T) {
	mr := NewMultiRegistryWithCache(nil, nil)
	// All 7 languages should be registered without caching.
	for _, lang := range []string{"go", "npm", "python", "rust", "java", "ruby", "csharp"} {
		if mr.ForLanguage(lang) == nil {
			t.Errorf("missing registry for %s", lang)
		}
	}
}

func TestNewMultiRegistryWithCache_WithCache(t *testing.T) {
	cache := newMemCache()
	mr := NewMultiRegistryWithCache(nil, cache)
	// All registries should be CachedRegistry.
	for _, lang := range []string{"go", "npm", "python", "rust", "java", "ruby", "csharp"} {
		reg := mr.ForLanguage(lang)
		if _, ok := reg.(*CachedRegistry); !ok {
			t.Errorf("%s: expected *CachedRegistry, got %T", lang, reg)
		}
	}
}
