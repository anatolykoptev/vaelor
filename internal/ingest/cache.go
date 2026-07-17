package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
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

// ingestL2KeyVersion is embedded in the Redis key prefix so a wire-format
// or struct-shape change can invalidate stale L2 entries by bumping it.
const ingestL2KeyVersion = "v1"
const ingestL2Prefix = "gc:ingest:" + ingestL2KeyVersion + ":"

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

// wireIngestEntry is the gob-friendly on-wire format for an L2 entry.
// It intentionally contains only the data needed to reconstruct the L1
// entry; the cache key itself encodes the opts.
type wireIngestEntry struct {
	Result      *IngestResult
	ContentHash string
	ValidatedAt time.Time
}

// ingestRepoCache is an in-memory LRU+TTL cache for IngestRepo results.
type ingestRepoCache struct {
	mu      sync.Mutex
	entries map[string]*ingestCacheEntry
	order   []string // LRU order: front = least recently used
	l2      kitcache.L2
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

// SetL2 wires the process-level ingest cache to Redis. Passing an empty
// redisURL disables L2. Called once from cmd/go-code/register.go at startup.
func SetL2(redisURL string) {
	var l2 kitcache.L2
	if redisURL != "" {
		l2 = kitcache.NewRedisL2(redisURL, 0, ingestL2Prefix)
	}
	ingestCache.mu.Lock()
	defer ingestCache.mu.Unlock()
	ingestCache.l2 = l2
}

// get returns a cached entry if it exists and is still valid (TTL not
// expired AND content hash unchanged). Returns nil on miss.
//
// The returned *IngestResult has a FRESH Files slice (shallow copy of the
// slice header + element pointers) so callers that mutate ir.Files (e.g.
// AnalyzeForResearch's test-file filtering via ir.Files[:0] truncation)
// don't corrupt the cached entry for subsequent callers. The []*File
// elements themselves are shared (read-only), which is safe.
func (c *ingestRepoCache) get(key string, root string) *IngestResult {
	c.mu.Lock()
	if res := c.getL1Locked(key, root); res != nil {
		c.mu.Unlock()
		return res
	}
	c.mu.Unlock()

	if c.l2 == nil {
		return nil
	}

	data, err := c.l2.Get(context.Background(), key)
	if err != nil {
		return nil
	}

	entry, err := decodeIngestEntry(data)
	if err != nil {
		return nil
	}

	// Validate content hash before trusting a cross-process L2 entry.
	currentHash := RepoContentHash(root)
	if currentHash != entry.contentHash {
		// Stale L2 entry; delete it to avoid serving it again.
		_ = c.l2.Del(context.Background(), key)
		return nil
	}

	c.mu.Lock()
	c.insertEntryLocked(key, entry)
	res := c.copyResultLocked(entry.result)
	c.mu.Unlock()
	return res
}

// getL1Locked returns a valid L1 hit or nil. Caller must hold c.mu.
func (c *ingestRepoCache) getL1Locked(key string, root string) *IngestResult {
	e, ok := c.entries[key]
	if !ok {
		return nil
	}

	// TTL check.
	if time.Since(e.validatedAt) > ingestCacheTTL {
		c.deleteEntryLocked(key)
		return nil
	}

	// Content-hash check: re-walk to compute the current hash and compare.
	// This is the same walk scipCache does; it's ~10ms for 100 files and
	// avoids serving stale results after a git checkout.
	currentHash := RepoContentHash(root)
	if currentHash != e.contentHash {
		c.deleteEntryLocked(key)
		return nil
	}

	// Move to back of LRU (most recently used).
	c.touch(key)

	// Return a shallow copy with a fresh Files slice so callers can
	// truncate/append without corrupting the cached entry.
	return c.copyResultLocked(e.result)
}

// put stores a new entry, evicting the least-recently-used if at capacity.
//
// The stored entry is a defensive copy (fresh Files slice) so the caller
// can mutate the returned *IngestResult without corrupting the cache.
// get() also copies on return, giving each caller its own slice.
func (c *ingestRepoCache) put(key string, result *IngestResult, root string) {
	// Compute content hash outside the lock so concurrent callers aren't
	// blocked on the filesystem walk.
	hash := RepoContentHash(root)

	// Defensive copy: store an independent Files slice so the caller
	// (who gets the same *IngestResult pointer from IngestRepo) can
	// truncate/append without corrupting the cached entry.
	stored := *result
	stored.Files = make([]*File, len(result.Files))
	copy(stored.Files, result.Files)

	entry := &ingestCacheEntry{
		result:      &stored,
		contentHash: hash,
		validatedAt: time.Now(),
		opts:        optsKey(IngestOpts{Root: root}), // opts baked into key
	}

	c.mu.Lock()
	c.insertEntryLocked(key, entry)
	l2 := c.l2
	c.mu.Unlock()

	if l2 != nil {
		if data, err := encodeIngestEntry(entry); err == nil {
			_ = l2.Set(context.Background(), key, data, ingestCacheTTL)
		}
	}
}

// insertEntryLocked stores entry under key and evicts LRU if over capacity.
// Caller must hold c.mu.
func (c *ingestRepoCache) insertEntryLocked(key string, entry *ingestCacheEntry) {
	c.entries[key] = entry
	// Move key to the back if it already exists, or append.
	c.removeOrder(key)
	c.order = append(c.order, key)

	// Evict LRU if over capacity.
	for len(c.entries) > ingestCacheMaxEntries && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

// deleteEntryLocked removes key from L1 maps. Caller must hold c.mu.
func (c *ingestRepoCache) deleteEntryLocked(key string) {
	delete(c.entries, key)
	c.removeOrder(key)
}

// removeOrder removes key from the LRU order slice. Caller must hold c.mu.
func (c *ingestRepoCache) removeOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// copyResultLocked returns a shallow copy of result with a fresh Files slice.
// Caller must hold c.mu.
func (c *ingestRepoCache) copyResultLocked(result *IngestResult) *IngestResult {
	r := *result
	r.Files = make([]*File, len(result.Files))
	copy(r.Files, result.Files)
	return &r
}

// touch moves key to the back of the LRU order slice (most recently used).
func (c *ingestRepoCache) touch(key string) {
	c.removeOrder(key)
	c.order = append(c.order, key)
}

// Reset clears all cached entries. Exposed for tests.
func ResetCache() {
	ingestCache.mu.Lock()
	defer ingestCache.mu.Unlock()
	ingestCache.entries = make(map[string]*ingestCacheEntry, ingestCacheMaxEntries)
	ingestCache.order = nil
}

// RepoContentHash computes a content-based hash of a repository's source
// files, mirroring scip.CacheKey's approach: walk the tree, hash the first
// 4KB of each non-hidden, non-.git file, and fold (relpath, digest) pairs
// into a single SHA256. Content-based (not mtime) so git checkout cycles
// don't cause false invalidation.
func RepoContentHash(root string) string {
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

// encodeIngestEntry serializes an ingest cache entry to []byte using gob.
func encodeIngestEntry(e *ingestCacheEntry) ([]byte, error) {
	var buf bytes.Buffer
	w := wireIngestEntry{
		Result:      e.result,
		ContentHash: e.contentHash,
		ValidatedAt: e.validatedAt,
	}
	if err := gob.NewEncoder(&buf).Encode(w); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeIngestEntry inverts encodeIngestEntry.
func decodeIngestEntry(data []byte) (*ingestCacheEntry, error) {
	var w wireIngestEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return nil, err
	}
	return &ingestCacheEntry{
		result:      w.Result,
		contentHash: w.ContentHash,
		validatedAt: w.ValidatedAt,
	}, nil
}
