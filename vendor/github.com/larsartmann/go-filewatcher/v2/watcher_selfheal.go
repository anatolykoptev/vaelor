package filewatcher

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

// selfHealLoop periodically retries failed watch registrations.
// It runs when WithSelfHeal(interval) is configured. Failed paths are
// retried at the configured interval until they succeed or the watcher
// is closed. Successfully re-registered paths are added to the watch list
// and will receive subsequent fsnotify events.
func (w *Watcher) selfHealLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.selfHealInterval)
	defer ticker.Stop()

	w.debugLog(
		"self-heal loop started",
		slog.Duration("interval", w.selfHealInterval),
	)

	for {
		select {
		case <-ctx.Done():
			w.debugLog("self-heal loop exiting: context cancelled")

			return
		case <-w.done:
			w.debugLog("self-heal loop exiting: watcher done")

			return
		case <-ticker.C:
			w.attemptSelfHeal()
		}
	}
}

// attemptSelfHeal tries to re-add all paths that previously failed to register.
// Successfully re-registered paths are removed from the failedPaths set.
//
// The watcher mutex is held only briefly to snapshot the failed paths set.
// fsnotify.Add and append operations are done without holding the lock to
// avoid blocking other watcher operations.
func (w *Watcher) attemptSelfHeal() {
	w.mu.Lock()

	if len(w.failedPaths) == 0 {
		w.mu.Unlock()

		return
	}

	// Snapshot the failed paths to avoid holding the lock during fsnotify operations.
	paths := make([]string, 0, len(w.failedPaths))
	for p := range w.failedPaths {
		paths = append(paths, p)
	}

	w.mu.Unlock()

	healed := 0

	for _, path := range paths {
		// Skip if already watched (e.g., added by another path).
		if w.isPathWatched(path) {
			w.removeFailedPath(path)

			continue
		}

		addErr := w.fswatcher.Add(path)
		if addErr == nil {
			w.removeFailedPath(path)
			w.appendToWatchList(path)

			healed++

			if w.onAdd != nil {
				w.onAdd(path)
			}
		}
	}

	if healed > 0 {
		remaining := w.failedPathCount()

		w.debugLog(
			"self-heal: re-registered paths",
			slog.Int("count", healed),
			slog.Int("remaining_failed", remaining),
		)
	}
}

// isPathWatched reports whether path is already in the watch list.
func (w *Watcher) isPathWatched(path string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return slices.Contains(w.watchList, path)
}

// appendToWatchList adds path to the watch list under mutex protection.
func (w *Watcher) appendToWatchList(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.watchList = append(w.watchList, path)
}

// removeFailedPath removes a path from the failed set under mutex protection.
func (w *Watcher) removeFailedPath(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.failedPaths, path)
}

// failedPathCount returns the number of paths still failing.
func (w *Watcher) failedPathCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return len(w.failedPaths)
}
