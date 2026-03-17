package scip

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	if err := copyFile(indexPath, dst); err != nil {
		return fmt.Errorf("scip cache put %q: %w", key, err)
	}
	return nil
}

// CacheKey computes a 16-hex-char key from the mtimes of source files in dir.
// Hidden files and the .git directory are skipped for speed.
func CacheKey(dir string) string {
	type entry struct {
		rel   string
		mtime int64
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
			info, err := de.Info()
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(dir, full)
			if err != nil {
				continue
			}
			entries = append(entries, entry{rel: rel, mtime: info.ModTime().UnixNano()})
		}
	}
	walk(dir, 0)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rel < entries[j].rel
	})

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s:%d\n", e.rel, e.mtime)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// entryPath returns the filesystem path for a cache entry.
func (c *Cache) entryPath(key string) string {
	return filepath.Join(c.dir, key+".scip")
}

// copyFile copies the file at src to dst, creating dst if needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return out.Close()
}
