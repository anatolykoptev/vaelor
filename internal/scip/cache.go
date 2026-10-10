package scip

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/fsutil"
)

// Cache stores SCIP index files keyed by a content hash.
type Cache struct {
	dir string
}

// NewCache creates a Cache backed by dir, creating it if needed.
func NewCache(dir string) *Cache {
	_ = os.MkdirAll(dir, 0o755)
	return &Cache{dir: dir}
}

// Get returns the path to a cached index.scip file for the given key.
// Returns ("", false) on a cache miss.
func (c *Cache) Get(key string) (string, bool) {
	p := c.entryPath(key)
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

// Put copies the index file at indexPath into the cache under key.
func (c *Cache) Put(key, indexPath string) error {
	dst := c.entryPath(key)
	if err := copyFilePath(indexPath, dst); err != nil {
		return fmt.Errorf("scip cache put %q: %w", key, err)
	}
	return nil
}

const cacheKeyLen = 16 // hex chars of SHA256 for cache key

// contentHashChunk is the maximum number of bytes read from each file for
// the content-based cache key. 4KB is enough to detect changes in most
// source files while keeping the walk fast (~10ms for 100 files).
const contentHashChunk = 4096

// CacheKey computes a 16-hex-char key from the CONTENT of source files in dir.
// Hidden files and the .git directory are skipped for speed.
//
// Content-based (not mtime-based) so that git checkout cycles don't cause
// false cache misses — switching branches back and forth changes mtimes
// even when file content is byte-identical.
func CacheKey(dir string) string {
	type entry struct {
		rel    string
		digest [sha256.Size]byte
	}
	var entries []entry

	const maxDepth = 10
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
			rel, err := filepath.Rel(dir, full)
			if err != nil {
				continue
			}
			digest := hashFileContent(dir, rel)
			entries = append(entries, entry{rel: rel, digest: digest})
		}
	}
	walk(dir, 0)

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
	return fmt.Sprintf("%x", h.Sum(nil))[:cacheKeyLen]
}

// hashFileContent returns a SHA256 digest of the first contentHashChunk bytes
// of rel under root. Returns a zero digest on read error (the file is
// effectively skipped — its rel path still contributes to the cache key). The
// name comes from the checkout, so the read goes through fsutil: symlinks and
// special files are refused (and counted quietly) instead of being opened.
func hashFileContent(root, rel string) [sha256.Size]byte {
	var digest [sha256.Size]byte
	data, err := fsutil.ReadRepoFilePrefix(root, rel, contentHashChunk)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fsutil.CountRefusal("scip.hash", fsutil.Reason(err))
		}
		return digest
	}
	return sha256.Sum256(data)
}

// entryPath returns the filesystem path for a cache entry.
func (c *Cache) entryPath(key string) string {
	return filepath.Join(c.dir, key+".scip")
}
