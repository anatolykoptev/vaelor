package freshness

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Cache key prefix for dependency freshness lookups.
const cacheKeyPrefix = "dep:fresh"

// Default TTL for cached registry lookups.
const DefaultCacheTTL = 24 * time.Hour

// Cache provides a simple key-value cache interface for registry lookups.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// CachedRegistry wraps a Registry with cache-backed lookups.
// On cache miss or error, it falls back to the inner registry.
type CachedRegistry struct {
	inner Registry
	cache Cache
	ttl   time.Duration
	lang  string
}

// NewCachedRegistry returns a Registry that caches results from inner.
func NewCachedRegistry(inner Registry, cache Cache, lang string, ttl time.Duration) *CachedRegistry {
	return &CachedRegistry{
		inner: inner,
		cache: cache,
		ttl:   ttl,
		lang:  lang,
	}
}

// Latest returns the latest version for name, using cache when available.
// Cache errors are silently ignored — we fall back to the inner registry.
func (c *CachedRegistry) Latest(ctx context.Context, name string) (string, error) {
	key := fmt.Sprintf("%s:%s:%s", cacheKeyPrefix, c.lang, name)

	if val, err := c.cache.Get(ctx, key); err == nil && val != "" {
		return val, nil
	}

	latest, err := c.inner.Latest(ctx, name)
	if err != nil {
		return "", err
	}

	// Best-effort cache write — ignore errors.
	_ = c.cache.Set(ctx, key, latest, c.ttl)

	return latest, nil
}

// NewMultiRegistryWithCache creates a MultiRegistry, optionally wrapping
// each language registry with cache. Pass nil cache to skip caching.
func NewMultiRegistryWithCache(client *http.Client, cache Cache) *MultiRegistry {
	npmReg := &NpmRegistry{BaseURL: DefaultNpmURL, Client: client}
	regs := map[string]Registry{
		"go":         &GoRegistry{BaseURL: DefaultGoProxyURL, Client: client},
		"npm":        npmReg,
		"typescript": npmReg,
		"python":     &PyPIRegistry{BaseURL: DefaultPyPIURL, Client: client},
		"rust":       &CratesRegistry{BaseURL: DefaultCratesURL, Client: client},
		"java":       &MavenRegistry{BaseURL: DefaultMavenURL, Client: client},
		"ruby":       &RubyGemsRegistry{BaseURL: DefaultRubyGemsURL, Client: client},
		"csharp":     &NuGetRegistry{BaseURL: DefaultNuGetURL, Client: client},
	}

	if cache != nil {
		for lang, reg := range regs {
			regs[lang] = NewCachedRegistry(reg, cache, lang, DefaultCacheTTL)
		}
	}

	return &MultiRegistry{registries: regs}
}
