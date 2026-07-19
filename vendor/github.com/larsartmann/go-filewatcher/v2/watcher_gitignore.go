package filewatcher

import (
	"path/filepath"
	"strings"
	"sync"

	gitignore "github.com/sabhiram/go-gitignore"
)

// gitignoreCache stores compiled .gitignore matchers keyed by the directory
// that contains the .gitignore file. This allows hierarchical matching:
// a path is checked against all ancestor .gitignore files.
type gitignoreCache struct {
	mu       sync.RWMutex
	matchers map[string]*gitignore.GitIgnore // key: directory containing .gitignore
}

func newGitignoreCache() *gitignoreCache {
	return &gitignoreCache{
		mu:       sync.RWMutex{},
		matchers: make(map[string]*gitignore.GitIgnore),
	}
}

// load loads and caches a .gitignore file from the given directory.
// No-op if no .gitignore exists or loading fails.
func (c *gitignoreCache) load(dir string) {
	c.mu.RLock()

	if _, ok := c.matchers[dir]; ok {
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
	c.matchers[dir] = ignoreMatcher
	c.mu.Unlock()
}

// loadGitignoreForDir loads the .gitignore from the given directory if it exists.
// Called during walking to discover gitignore rules for each directory visited.
func (w *Watcher) loadGitignoreForDir(dir string) {
	if !w.gitignoreEnabled || w.gitignoreCache == nil {
		return
	}

	w.gitignoreCache.load(dir)
}

// shouldSkipByGitignore checks if a path should be skipped based on accumulated
// gitignore rules. Only checks matchers that are ancestors of the path
// (i.e., the path must be inside the gitignore directory).
func (w *Watcher) shouldSkipByGitignore(path string) bool {
	if !w.gitignoreEnabled || w.gitignoreCache == nil {
		return false
	}

	w.gitignoreCache.mu.RLock()
	defer w.gitignoreCache.mu.RUnlock()

	sep := string(filepath.Separator)

	for gitignoreDir, ignoreMatcher := range w.gitignoreCache.matchers {
		// Only check matchers from ancestor directories
		prefix := gitignoreDir + sep
		if !strings.HasPrefix(path, prefix) && path != gitignoreDir {
			continue
		}

		relPath, err := filepath.Rel(gitignoreDir, path)
		if err != nil {
			continue
		}

		if ignoreMatcher.MatchesPath(relPath) {
			return true
		}
	}

	return false
}
