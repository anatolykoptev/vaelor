package filewatcher

import (
	"path/filepath"
	"strings"
	"sync"

	gitignore "github.com/sabhiram/go-gitignore"
)

// gitignoreCache stores compiled .gitignore matchers keyed by the canonical
// (case-aware, NFC-normalized) path key of the directory that contains the
// .gitignore file. This allows hierarchical matching: a path is checked
// against all ancestor .gitignore files.
type gitignoreCache struct {
	mu       sync.RWMutex
	matchers map[string]*gitignore.GitIgnore // key: pathKey of directory containing .gitignore
}

func newGitignoreCache() *gitignoreCache {
	return &gitignoreCache{
		mu:       sync.RWMutex{},
		matchers: make(map[string]*gitignore.GitIgnore),
	}
}

// load loads and caches a .gitignore file from the given directory.
// dir is the original filesystem path (used to read the .gitignore file);
// key is the canonical pathKey used for map storage and lookup.
// No-op if no .gitignore exists or loading fails.
func (c *gitignoreCache) load(dir string, key string) {
	c.mu.RLock()

	if _, ok := c.matchers[key]; ok {
		c.mu.RUnlock()

		return
	}

	c.mu.RUnlock()

	gitignorePath := filepath.Join(dir, ".gitignore")

	ignoreMatcher, compileErr := gitignore.CompileIgnoreFile(gitignorePath)
	if compileErr != nil {
		return
	}

	c.mu.Lock()
	c.matchers[key] = ignoreMatcher
	c.mu.Unlock()
}

// loadGitignoreForDir loads the .gitignore from the given directory if it exists.
// Called during walking to discover gitignore rules for each directory visited.
func (w *Watcher) loadGitignoreForDir(dir string) {
	if !w.gitignoreEnabled || w.gitignoreCache == nil {
		return
	}

	w.gitignoreCache.load(dir, w.pathKey(dir))
}

// shouldSkipByGitignore checks if a path should be skipped based on accumulated
// gitignore rules. Only checks matchers that are ancestors of the path
// (i.e., the path must be inside the gitignore directory).
//
// Both the path and the gitignore directory keys are canonicalized via pathKey
// before comparison, so the ancestor-prefix check respects filesystem
// case-sensitivity and Unicode normalization.
func (w *Watcher) shouldSkipByGitignore(path string) bool {
	if !w.gitignoreEnabled || w.gitignoreCache == nil {
		return false
	}

	w.gitignoreCache.mu.RLock()
	defer w.gitignoreCache.mu.RUnlock()

	canonicalPath := w.pathKey(path)
	sep := string(filepath.Separator)

	for gitignoreKey, ignoreMatcher := range w.gitignoreCache.matchers {
		// Only check matchers from ancestor directories (using canonical keys)
		prefix := gitignoreKey + sep
		if !strings.HasPrefix(canonicalPath, prefix) && canonicalPath != gitignoreKey {
			continue
		}

		// Compute relative path using canonical forms so the ancestor
		// relationship is consistent on case-insensitive filesystems.
		relPath, err := filepath.Rel(gitignoreKey, canonicalPath)
		if err != nil { //nolint:erraudit // both paths are canonical keys; on Rel failure just skip this matcher
			continue
		}

		if ignoreMatcher.MatchesPath(relPath) {
			return true
		}
	}

	return false
}
