// Package sourcemap fetches and caches JavaScript source maps and resolves
// minified frames (url, line, column) → original (file, line, function).
package sourcemap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gosourcemap "github.com/go-sourcemap/sourcemap"
)

// Frame is a resolved original-source location.
type Frame struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Function string `json:"function,omitempty"`
}

// Resolver fetches and caches source maps. Safe for concurrent use.
type Resolver struct {
	client  *http.Client
	mu      sync.Mutex
	cache   map[string]*cacheEntry // key: map URL
	maxSize int
	ttl     time.Duration
}

type cacheEntry struct {
	parsed *gosourcemap.Consumer
	at     time.Time
}

// NewResolver creates a Resolver. maxSize is the FIFO-by-insertion-time bound
// (entries evicted oldest-first when full), ttl is per-entry expiry duration.
func NewResolver(client *http.Client, maxSize int, ttl time.Duration) *Resolver {
	return &Resolver{
		client:  client,
		cache:   make(map[string]*cacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Resolve takes the URL of a minified JS bundle plus 1-based line and column
// (matching browser stack-frame convention), fetches its companion .map file,
// and returns the original frame. Source map URL is derived as jsURL+".map".
//
// The returned Frame.File is the path component of the resolved source: the
// go-sourcemap library resolves relative source paths against the map URL's
// origin, so we strip that origin prefix to return a clean relative path.
func (r *Resolver) Resolve(ctx context.Context, jsURL string, line, column int) (*Frame, error) {
	mapURL := jsURL + ".map"
	consumer, err := r.consumerFor(ctx, mapURL)
	if err != nil {
		return nil, fmt.Errorf("fetch sourcemap: %w", err)
	}
	src, name, oline, ocol, ok := consumer.Source(line, column)
	if !ok {
		return nil, fmt.Errorf("no mapping for %s:%d:%d", jsURL, line, column)
	}
	// Strip origin+directory so callers get a relative path (e.g. "src/foo.svelte")
	// rather than an absolute URL ("http://host/_app/chunks/src/foo.svelte").
	src = stripBase(src, mapURL)
	return &Frame{
		File:     src,
		Line:     oline,
		Column:   ocol,
		Function: name,
	}, nil
}

// stripBase removes the scheme+host+directory prefix from resolved so that
// callers receive a path relative to the map URL's directory — matching the
// raw entry in sources[]. The go-sourcemap library resolves relative source
// paths against the map URL, so "src/foo.svelte" in a map at
// "http://host/_app/chunks/app.js.map" becomes
// "http://host/_app/chunks/src/foo.svelte" after parsing; this function
// reverses that transformation.
func stripBase(resolved, mapURL string) string {
	u, err := url.Parse(mapURL)
	if err != nil {
		return resolved
	}
	// Build the base: scheme://host + directory of the map path.
	dir := u.Scheme + "://" + u.Host
	if idx := strings.LastIndex(u.Path, "/"); idx >= 0 {
		dir += u.Path[:idx+1]
	} else {
		dir += "/"
	}
	if strings.HasPrefix(resolved, dir) {
		return resolved[len(dir):]
	}
	// Fallback: strip just the origin.
	origin := u.Scheme + "://" + u.Host
	if strings.HasPrefix(resolved, origin+"/") {
		return resolved[len(origin)+1:]
	}
	return resolved
}

func (r *Resolver) consumerFor(ctx context.Context, mapURL string) (*gosourcemap.Consumer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.cache[mapURL]; ok {
		if time.Since(e.at) < r.ttl {
			return e.parsed, nil
		}
		delete(r.cache, mapURL)
	}

	// Fetch and parse inside the lock. The cache is small (N=64) and misses are
	// infrequent, so holding the lock through the network round-trip is cheaper
	// than the TOCTOU window introduced by a double-checked unlock pattern.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mapURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	const maxBodyBytes = 16 << 20 // 16 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	consumer, err := gosourcemap.Parse(mapURL, body)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if len(r.cache) >= r.maxSize {
		// Evict oldest entry.
		var oldestKey string
		var oldestAt time.Time
		first := true
		for k, v := range r.cache {
			if first || v.at.Before(oldestAt) {
				oldestAt = v.at
				oldestKey = k
				first = false
			}
		}
		delete(r.cache, oldestKey)
	}
	r.cache[mapURL] = &cacheEntry{parsed: consumer, at: time.Now()}
	return consumer, nil
}

// IsAllowedURL reports whether u has an http or https scheme and its host
// exactly matches one of the allowedHosts entries. Subdomain-prefix attacks
// (evil.com/cdn.example.com/...) and suffix attacks
// (cdn.example.com.evil.io/...) are both rejected by the trailing-slash anchor.
func IsAllowedURL(u string, allowedHosts []string) bool {
	for _, h := range allowedHosts {
		if strings.HasPrefix(u, "https://"+h+"/") || strings.HasPrefix(u, "http://"+h+"/") {
			return true
		}
	}
	return false
}
