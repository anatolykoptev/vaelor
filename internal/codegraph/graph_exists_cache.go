package codegraph

import (
	"sync"
	"time"
)

// graphExistsCache caches positive graph-existence checks to avoid
// hammering ag_catalog.ag_graph on every read-path query.
//
// Only positive results are cached — a graph that does not exist
// today may be created by IndexRepo at any moment, so we want
// the next check to see it. Positive entries are valid for ttl;
// after expiry we re-check.
type graphExistsCache struct {
	mu   sync.RWMutex
	seen map[string]time.Time
	ttl  time.Duration
}

func newGraphExistsCache(ttl time.Duration) *graphExistsCache {
	return &graphExistsCache{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// Hit reports cache hit (graph known to exist within TTL).
func (c *graphExistsCache) Hit(name string) bool {
	c.mu.RLock()
	t, ok := c.seen[name]
	c.mu.RUnlock()
	if !ok {
		existsCacheMissTotal.Inc()
		return false
	}
	if time.Since(t) > c.ttl {
		existsCacheMissTotal.Inc()
		return false
	}
	existsCacheHitTotal.Inc()
	return true
}

// Mark records that graph exists at this moment.
func (c *graphExistsCache) Mark(name string) {
	c.mu.Lock()
	c.seen[name] = time.Now()
	c.mu.Unlock()
}

// Forget removes the entry — call after a known-failed cypher
// (graph dropped between preflight and query) so next call re-probes.
func (c *graphExistsCache) Forget(name string) {
	c.mu.Lock()
	delete(c.seen, name)
	c.mu.Unlock()
	existsCacheForgetTotal.Inc()
}
