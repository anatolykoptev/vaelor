package callgraph

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	cgCacheTTL     = 5 * time.Minute
	cgCacheMaxSize = 5
)

// cgL2KeyVersion is embedded in the Redis key prefix so a wire-format or
// struct-shape change can invalidate stale L2 entries by bumping it.
const cgL2KeyVersion = "v1"
const cgL2Prefix = "gc:callgraph:" + cgL2KeyVersion + ":"

// cgCacheEntry holds a cached CallGraph and when it was computed.
type cgCacheEntry struct {
	cg *CallGraph
	at time.Time
}

// wireCGEntry is the gob-friendly on-wire format for an L2 entry.
type wireCGEntry struct {
	CG          *CallGraph
	At          time.Time
	ContentHash string
}

// callGraphCache is a small TTL+LRU cache for BuildFromRepo results.
// Parsing all repo files is expensive (15-60s); caching avoids re-parsing
// when multiple tools analyze the same repo in quick succession.
type callGraphCache struct {
	mu  sync.Mutex
	lru *cache.LRU[string, cgCacheEntry]
	l2  kitcache.L2
}

var cgCache = &callGraphCache{
	lru: cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize),
}

// SetL2 wires the process-level callgraph cache to Redis. Passing an empty
// redisURL disables L2. Called once from cmd/go-code/register.go at startup.
func SetL2(redisURL string) {
	var l2 kitcache.L2
	if redisURL != "" {
		l2 = kitcache.NewRedisL2(redisURL, 0, cgL2Prefix)
	}
	cgCache.mu.Lock()
	defer cgCache.mu.Unlock()
	cgCache.l2 = l2
}

func (c *callGraphCache) get(key, root string) (*CallGraph, bool) {
	c.mu.Lock()
	e, ok := c.lru.Get(key)
	if ok {
		if time.Since(e.at) <= cgCacheTTL {
			c.mu.Unlock()
			return e.cg, true
		}
		c.lru.Delete(key)
	}
	c.mu.Unlock()

	if c.l2 == nil {
		return nil, false
	}

	data, err := c.l2.Get(context.Background(), key)
	if err != nil {
		return nil, false
	}

	entry, err := decodeCGEntry(data)
	if err != nil {
		return nil, false
	}

	// Validate content hash before trusting a cross-process L2 entry.
	if ingest.RepoContentHash(root) != entry.ContentHash {
		// Stale L2 entry; delete it to avoid serving it again.
		_ = c.l2.Del(context.Background(), key)
		return nil, false
	}

	c.mu.Lock()
	c.lru.Set(key, cgCacheEntry{cg: entry.CG, at: entry.At})
	cg := entry.CG
	c.mu.Unlock()
	return cg, true
}

func (c *callGraphCache) set(key string, cg *CallGraph, root string) {
	hash := ingest.RepoContentHash(root)
	at := time.Now()

	c.mu.Lock()
	c.lru.Set(key, cgCacheEntry{cg: cg, at: at})
	l2 := c.l2
	c.mu.Unlock()

	if l2 != nil {
		if data, err := encodeCGEntry(cg, at, hash); err == nil {
			_ = l2.Set(context.Background(), key, data, cgCacheTTL)
		}
	}
}

// cgCacheKey produces a stable cache key from TraceRepoInput fields
// that affect the result: root path, language filter, focus path, and
// the field-access opt-in (changes which edges land in the graph).
func cgCacheKey(input TraceRepoInput) string {
	return fmt.Sprintf("%s::%s::%s::fa=%t", input.Root, input.Language, input.Focus, input.IncludeFieldAccess)
}

// InvalidateBuildCache clears the entire BuildFromRepo cache.
// Used in tests and when a rebuild is explicitly requested.
func InvalidateBuildCache() {
	cgCache.mu.Lock()
	defer cgCache.mu.Unlock()
	cgCache.lru = cache.NewLRU[string, cgCacheEntry](cgCacheMaxSize)
}

// encodeCGEntry serializes a CallGraph, timestamp and content hash to []byte using gob.
func encodeCGEntry(cg *CallGraph, at time.Time, contentHash string) ([]byte, error) {
	var buf bytes.Buffer
	w := wireCGEntry{CG: cg, At: at, ContentHash: contentHash}
	if err := gob.NewEncoder(&buf).Encode(w); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeCGEntry inverts encodeCGEntry.
func decodeCGEntry(data []byte) (*wireCGEntry, error) {
	var w wireCGEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return nil, err
	}
	relinkCallGraphSymbols(w.CG)
	return &w, nil
}

// symbolKey is the stable identifier BuildCallGraphWithOpts uses indirectly
// to locate a symbol (name, file, and line range). We use it after gob decode
// to collapse separate Caller/Callee pointer copies back into the canonical
// *parser.Symbol values from the decoded Symbols slice.
type symbolKey struct {
	Language  string
	File      string
	StartLine uint32
	EndLine   uint32
	Name      string
	Kind      parser.NodeKind
	Receiver  string
}

func makeSymbolKey(s *parser.Symbol) symbolKey {
	return symbolKey{
		Language:  s.Language,
		File:      s.File,
		StartLine: s.StartLine,
		EndLine:   s.EndLine,
		Name:      s.Name,
		Kind:      s.Kind,
		Receiver:  s.Receiver,
	}
}

// relinkCallGraphSymbols rewrites every Edge.Caller and Edge.Callee to point
// at the matching *parser.Symbol from cg.Symbols. gob decodes each pointer
// separately, so without this step map[ *parser.Symbol ] consumers see the
// cached graph as childless / everything-dead.
func relinkCallGraphSymbols(cg *CallGraph) {
	if cg == nil || len(cg.Symbols) == 0 {
		return
	}

	idx := make(map[symbolKey]*parser.Symbol, len(cg.Symbols))
	for _, sym := range cg.Symbols {
		k := makeSymbolKey(sym)
		if _, ok := idx[k]; !ok {
			idx[k] = sym
		}
	}

	for i := range cg.Edges {
		e := &cg.Edges[i]
		if e.Caller != nil {
			if sym, ok := idx[makeSymbolKey(e.Caller)]; ok {
				e.Caller = sym
			}
		}
		if e.Callee != nil {
			if sym, ok := idx[makeSymbolKey(e.Callee)]; ok {
				e.Callee = sym
			}
		}
	}
}
