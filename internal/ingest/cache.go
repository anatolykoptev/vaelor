package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ingestCacheTTL is how long a cached IngestResult stays valid before a
// content-hash re-check is required. Short enough that a git checkout or
// file edit during a session invalidates the entry; long enough that 12
// tool calls against the same repo in one session all hit the cache.
const ingestCacheTTL = 5 * time.Minute

// ingestCacheMaxEntries bounds memory: each entry holds a *IngestResult
// (file list + metadata, NOT file contents), so ~1KB per file × 10k files
// = ~10MB worst case per entry. 32 entries = ~320MB cap.
const ingestCacheMaxEntries = 32

// Process-level IngestRepo cache. Eliminates redundant filesystem walks
// when multiple tools (call_trace, code_graph, analyze, explore, compare,
// codesearch, embeddings, …) are called against the same repo in one
// session. See issue #464.
var ingestCache = &ingestRepoCache{
	entries: make(map[string]*ingestCacheEntry, ingestCacheMaxEntries),
}

// ingestCacheEntry holds a cached IngestResult plus its content hash and
// the time it was validated.
type ingestCacheEntry struct {
	result      *IngestResult
	contentHash string
	validatedAt time.Time
	opts        ingestOptsKey // the opts that produced this entry
}

// ingestRepoCache is an in-memory LRU+TTL cache for IngestRepo results.
type ingestRepoCache struct {
	mu      sync.Mutex
	entries map[string]*ingestCacheEntry
	order   []string // LRU order: front = least recently used
}

// ingestOptsKey is the cache-key component derived from IngestOpts. Two
// calls with different Focus/Languages/MaxFileBytes/MaxFiles/FollowSymlinks/
// ExcludeTests produce different keys, so they don't collide.
type ingestOptsKey struct {
	Focus          string
	Languages      string // sorted, comma-joined
	MaxFileBytes   int64
	MaxFiles       int
	FollowSymlinks bool
	ExcludeTests   bool
}

func optsKey(opts IngestOpts) ingestOptsKey {
	langs := make([]string, len(opts.Languages))
	copy(langs, opts.Languages)
	sort.Strings(langs)
	return ingestOptsKey{
		Focus:          opts.Focus,
		Languages:      strings.Join(langs, ","),
		MaxFileBytes:   opts.MaxFileBytes,
		MaxFiles:       opts.MaxFiles,
		FollowSymlinks: opts.FollowSymlinks,
		ExcludeTests:   opts.ExcludeTests,
	}
}

// cacheKey builds the full cache key: root + opts hash. The content hash
// is computed separately (and only on a candidate hit) because it requires
// a filesystem walk — we want to avoid it on a clear miss.
func cacheKey(root string, opts IngestOpts) string {
	ok := optsKey(opts)
	h := sha256.New()
	h.Write([]byte(root))
	h.Write([]byte{0})
	h.Write([]byte(ok.Focus))
	h.Write([]byte{0})
	h.Write([]byte(ok.Languages))
	h.Write([]byte{0})
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(ok.MaxFileBytes))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(ok.MaxFiles))
	h.Write(buf[:])
	if ok.FollowSymlinks {
		h.Write([]byte{1})
	}
	if ok.ExcludeTests {
		h.Write([]byte{1})
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:32]
}

// get returns a cached entry if it exists and is still valid (TTL not
// expired AND content hash unchanged). Returns nil on miss.
func (c *ingestRepoCache) get(key string, root string) *IngestResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return nil
	}

	// TTL check.
	if time.Since(e.validatedAt) > ingestCacheTTL {
		return nil
	}

	// Content-hash check: re-walk to compute the current hash and compare.
	// This is the same walk scipCache does; it's ~10ms for 100 files and
	// avoids serving stale results after a git checkout.
	currentHash := repoContentHash(root)
	if currentHash != e.contentHash {
		return nil
	}

	// Move to back of LRU (most recently used).
	c.touch(key)

	return e.result
}

// put stores a new entry, evicting the least-recently-used if at capacity.
func (c *ingestRepoCache) put(key string, result *IngestResult, root string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Compute content hash now so future get() calls can compare without
	// re-walking (until TTL expires).
	hash := repoContentHash(root)

	c.entries[key] = &ingestCacheEntry{
		result:      result,
		contentHash: hash,
		validatedAt: time.Now(),
		opts:        optsKey(IngestOpts{Root: root}), // opts baked into key
	}
	c.order = append(c.order, key)

	// Evict LRU if over capacity.
	for len(c.entries) > ingestCacheMaxEntries && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

// touch moves key to the back of the LRU order slice (most recently used).
func (c *ingestRepoCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			break
		}
	}
}

// Reset clears all cached entries. Exposed for tests.
func ResetCache() {
	ingestCache.mu.Lock()
	defer ingestCache.mu.Unlock()
	ingestCache.entries = make(map[string]*ingestCacheEntry, ingestCacheMaxEntries)
	ingestCache.order = nil
}

// repoContentHash computes a content-based hash of a repository's source
// files, mirroring scip.CacheKey's approach: walk the tree, hash the first
// 4KB of each non-hidden, non-.git file, and fold (relpath, digest) pairs
// into a single SHA256. Content-based (not mtime) so git checkout cycles
// don't cause false invalidation.
func repoContentHash(root string) string {
	type entry struct {
		rel    string
		digest [sha256.Size]byte
	}
	var entries []entry

	const maxDepth = 10
	const chunkSize = 4096

	var walk func(string, int)
	walk = func(current string, depth int) {
		if depth > maxDepth {
			return
		}
		des, err := os.ReadDir(current)
		if err != nil {
			return
		}
		for _, de := range des {
			name := de.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(current, name)
			if de.IsDir() {
				walk(full, depth+1)
				continue
			}
			rel, err := filepath.Rel(root, full)
			if err != nil {
				continue
			}
			entries = append(entries, entry{rel: rel, digest: hashFileChunk(full, chunkSize)})
		}
	}
	walk(root, 0)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rel < entries[j].rel
	})

	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write([]byte{':'})
		h.Write(e.digest[:])
		h.Write([]byte{'\n'})
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:32]
}

// hashFileChunk returns a SHA256 digest of the first n bytes of path.
// Returns a zero digest on read error (the rel path still contributes).
func hashFileChunk(path string, n int64) [sha256.Size]byte {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}
	}
	defer f.Close()

	h := sha256.New()
	_, _ = io.CopyN(h, f, n)
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return digest
}
