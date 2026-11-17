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
	pathKey := w.pathKey(path)

	if _, alreadyWatched := w.watchListKeys[pathKey]; alreadyWatched {
		w.debugLog("path already watched, skipping", slog.String("path", path))

		return
	}

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
		w.failedPaths[pathKey] = path
		w.handleError(ErrorContext{
			Operation: opAddPath,
			Path:      path,
			Event:     nil,
			Retryable: true,
		}, fmt.Errorf("watching path %q: %w", path, addErr))

		return
	}

	delete(w.failedPaths, pathKey)
	w.addToWatchList(path)

	if w.onAdd != nil {
		w.onAdd(path)
	}
}

// walkAndAddPaths walks a directory tree and adds all directories to the watcher.
// Directories are collected during walking and added in batches to yield to
// event processing between batches. Caller must hold w.mu lock.
func (w *Watcher) walkAndAddPaths(root RootPath) error {
	// Track whether we're the top-level caller (responsible for init/cleanup).
	// Recursive calls from handleFollowedSymlink reuse the existing map.
	topLevel := w.symlinkVisited == nil
	if topLevel {
		w.symlinkVisited = make(map[string]struct{})
	}

	defer func() {
		if topLevel {
			w.symlinkVisited = nil
		}
	}()

	// Record the root so a symlink pointing back to it is detected as a cycle.
	rootKey := w.pathKey(root.Get())
	w.symlinkVisited[rootKey] = struct{}{}

	w.walkBatch = make([]string, 0, watchBatchSize)

	err := filepath.WalkDir(root.Get(), w.walkDirFunc)

	// Flush remaining batch
	if len(w.walkBatch) > 0 {
		w.addBatch(w.walkBatch)
	}

	w.walkBatch = nil

	if err != nil {
		return fmt.Errorf(
			"walking directory %q: %w",
			root,
			err,
		) //nolint:erraudit // root already identifies the directory; rootKey is its canonical form
	}

	// Track the root path only if it wasn't already added via addBatch.
	// filepath.WalkDir visits the root first, so it's already in watchList.
	if _, ok := w.watchListKeys[rootKey]; !ok {
		w.addToWatchList(root.Get())
	}

	return nil
}

// walkDirFunc is the WalkDirFunc for adding paths during directory traversal.
// When walkBatch is set, it collects paths into the batch for batched registration.
// When walkBatch is nil, it adds paths immediately (used by tests).
func (w *Watcher) walkDirFunc(path string, d os.DirEntry, walkErr error) error {
	if walkErr != nil {
		isDir := d != nil && d.IsDir()

		return fmt.Errorf("walking directory entry %q (isDir=%v): %w", path, isDir, walkErr)
	}

	if !d.IsDir() {
		return nil
	}

	if w.followSymlinks && d.Type()&os.ModeSymlink != 0 {
		return w.handleFollowedSymlink(path)
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

// handleFollowedSymlink resolves a symlink to its target, performs cycle
// detection, and recursively walks the target if it's a new directory.
func (w *Watcher) handleFollowedSymlink(path string) error {
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

	// Cycle detection: skip if this target was already visited via another
	// symlink or is the walk root. Prevents infinite recursion on symlink
	// cycles (e.g., /a/b -> /a).
	resolvedKey := w.pathKey(resolved)
	if _, visited := w.symlinkVisited[resolvedKey]; visited {
		w.debugLog("symlink cycle detected, skipping",
			slog.String("symlink", path),
			slog.String("target", resolved))

		return nil
	}

	w.symlinkVisited[resolvedKey] = struct{}{}

	return w.walkAndAddPaths(NewRootPath(resolved))
}

// shouldSkipDir checks if a directory should be skipped based on ignore rules.
// On case-insensitive filesystems, directory names are compared case-insensitively
// so that "BUILD" matches "build" and "Node_Modules" matches "node_modules".
func (w *Watcher) shouldSkipDir(name string) bool {
	if w.skipDotDirs && strings.HasPrefix(name, ".") && name != "." {
		return true
	}

	if w.effectiveCaseSensitivity == CaseInsensitive {
		return w.shouldSkipDirCaseInsensitive(name)
	}

	if slices.Contains(DefaultIgnoreDirs, name) {
		return true
	}

	return slices.Contains(w.ignoreDirNames, name)
}

// shouldSkipDirCaseInsensitive checks ignore rules with lowercased comparison.
func (w *Watcher) shouldSkipDirCaseInsensitive(name string) bool {
	lowered := strings.ToLower(name)

	for _, dir := range DefaultIgnoreDirs {
		if strings.ToLower(dir) == lowered {
			return true
		}
	}

	for _, dir := range w.ignoreDirNames {
		if strings.ToLower(dir) == lowered {
			return true
		}
	}

	return false
}

// shouldExcludePath checks if a path should be excluded based on absolute path matching.
// It matches exact paths and path prefixes (subtree exclusion).
func (w *Watcher) shouldExcludePath(path string) bool {
	if len(w.excludePaths) == 0 {
		return false
	}

	key := w.pathKey(path)

	_, exact := w.excludePaths[key]
	if exact {
		return true
	}

	sep := string(filepath.Separator)
	prefix := key + sep

	for excludedPath := range w.excludePaths {
		excludedKey := w.pathKey(excludedPath)

		if strings.HasPrefix(excludedKey, prefix) {
			return false // path is a parent of an excluded path, don't skip it
		}

		if strings.HasPrefix(key, excludedKey+sep) {
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
