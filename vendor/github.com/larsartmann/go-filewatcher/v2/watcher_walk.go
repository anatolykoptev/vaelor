//nolint:varnamelen // Idiomatic short names: d (DirEntry), op (operation)
package filewatcher

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// initDebouncer sets up the appropriate debouncer based on configuration.
func (w *Watcher) initDebouncer() {
	switch {
	case w.perPathDebounce > 0:
		w.debounceInterface = NewDebouncer(w.perPathDebounce)
	case w.globalDebounce > 0:
		w.debounceInterface = NewGlobalDebouncer(w.globalDebounce)
	}
}

// addPath adds a directory (and optionally its subdirectories) to the fsnotify watcher.
// It also appends the root path to the watchList.
func (w *Watcher) addPath(root RootPath) error {
	if !w.recursive {
		w.tryAddPath(root.Get())

		return nil
	}

	return w.walkAndAddPaths(root)
}

// tryAddPath attempts to add a single path to the fsnotify watcher.
// Centralizes the budget check, error handling, watchList update, failedPaths
// tracking, and onAdd callback so callers can simply invoke it and move on.
// Skips silently when the inotify budget is exhausted; otherwise handles
// fswatcher.Add failures via handleError and failedPaths (for self-heal).
func (w *Watcher) tryAddPath(path string) {
	if w.maxWatches > 0 && len(w.watchList) >= w.maxWatches {
		w.debugLog(
			"watch budget exhausted, skipping path",
			slog.String("path", path),
			slog.Int("max_watches", w.maxWatches),
			slog.Int("current_watches", len(w.watchList)),
		)

		return
	}

	addErr := w.fswatcher.Add(path)
	if addErr != nil {
		w.watchErrors.Add(1)
		w.failedPaths[path] = struct{}{}
		w.handleError(ErrorContext{
			Operation: opAddPath,
			Path:      path,
			Event:     nil,
			Retryable: true,
		}, fmt.Errorf("watching path %q: %w", path, addErr))

		return
	}

	delete(w.failedPaths, path)
	w.watchList = append(w.watchList, path)

	if w.onAdd != nil {
		w.onAdd(path)
	}
}

// walkAndAddPaths walks a directory tree and adds all directories to the watcher.
// Directories are collected during walking and added in batches to yield to
// event processing between batches. Caller must hold w.mu lock.
func (w *Watcher) walkAndAddPaths(root RootPath) error {
	w.walkBatch = make([]string, 0, watchBatchSize)

	err := filepath.WalkDir(root.Get(), w.walkDirFunc)

	// Flush remaining batch
	if len(w.walkBatch) > 0 {
		w.addBatch(w.walkBatch)
	}

	w.walkBatch = nil

	if err != nil {
		return fmt.Errorf("walking directory %q: %w", root, err)
	}

	// Track the root path only if it wasn't already added via addBatch.
	// filepath.WalkDir visits the root first, so it's already in watchList.
	if len(w.watchList) == 0 || w.watchList[len(w.watchList)-1] != root.Get() {
		w.watchList = append(w.watchList, root.Get())
	}

	return nil
}

// walkDirFunc is the WalkDirFunc for adding paths during directory traversal.
// When walkBatch is set, it collects paths into the batch for batched registration.
// When walkBatch is nil, it adds paths immediately (used by tests).
//
//nolint:cyclop // walk logic with multiple skip conditions
func (w *Watcher) walkDirFunc(path string, d os.DirEntry, walkErr error) error {
	if walkErr != nil {
		isDir := d != nil && d.IsDir()

		return fmt.Errorf("walking directory entry %q (isDir=%v): %w", path, isDir, walkErr)
	}

	if !d.IsDir() {
		return nil
	}

	if w.followSymlinks && d.Type()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolving symlink %q: %w", path, err)
		}

		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("stat resolved symlink target %q: %w", resolved, err)
		}

		if !info.IsDir() {
			return nil
		}

		return w.walkAndAddPaths(NewRootPath(resolved))
	}

	if w.shouldSkipDir(d.Name()) {
		return filepath.SkipDir
	}

	if w.shouldExcludePath(path) {
		return filepath.SkipDir
	}

	w.loadGitignoreForDir(path)

	if w.shouldSkipByGitignore(path) {
		return filepath.SkipDir
	}

	// Batched mode: collect path for later batched addition
	if w.walkBatch != nil {
		w.walkBatch = append(w.walkBatch, path)

		if len(w.walkBatch) >= watchBatchSize {
			w.addBatch(w.walkBatch)
			w.walkBatch = w.walkBatch[:0]
		}

		return nil
	}

	// Direct mode: add immediately (used by tests and depth-limited walking)
	w.tryAddPath(path)

	return nil
}

// shouldSkipDir checks if a directory should be skipped based on ignore rules.
func (w *Watcher) shouldSkipDir(name string) bool {
	if w.skipDotDirs && strings.HasPrefix(name, ".") && name != "." {
		return true
	}

	if slices.Contains(DefaultIgnoreDirs, name) {
		return true
	}

	return slices.Contains(w.ignoreDirNames, name)
}

// shouldExcludePath checks if a path should be excluded based on absolute path matching.
// It matches exact paths and path prefixes (subtree exclusion).
func (w *Watcher) shouldExcludePath(path string) bool {
	if len(w.excludePaths) == 0 {
		return false
	}

	_, exact := w.excludePaths[path]
	if exact {
		return true
	}

	prefix := path + string(filepath.Separator)

	for excludedPath := range w.excludePaths {
		if strings.HasPrefix(excludedPath, prefix) {
			return false // path is a parent of an excluded path, don't skip it
		}

		if strings.HasPrefix(path, excludedPath+string(filepath.Separator)) {
			return true // path is under an excluded subtree
		}
	}

	return false
}

const watchBatchSize = 1000

// addBatch adds a batch of paths to the fsnotify watcher.
// Respects the maxWatches budget — stops adding when budget is exhausted.
func (w *Watcher) addBatch(paths []string) {
	for _, p := range paths {
		w.tryAddPath(p)
	}

	runtime.Gosched()
}

// detectMaxWatches reads the system inotify watch limit from /proc/sys/fs/inotify/max_user_watches.
// Returns 0 on non-Linux systems or if detection fails (meaning unlimited).
func detectMaxWatches() int {
	const procPath = "/proc/sys/fs/inotify/max_user_watches"

	data, err := os.ReadFile(procPath)
	if err != nil {
		return 0
	}

	n, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil {
		return 0
	}

	return n
}
