package filewatcher

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileState holds the metadata snapshot for a single file/directory.
type fileState struct {
	path    string // original filesystem path (used for event emission)
	modTime time.Time
	size    int64
	isDir   bool
}

// pollLoop runs a periodic filesystem poll to detect changes that fsnotify
// may miss (NFS, FUSE, Docker volumes, etc.). It maintains a snapshot of
// file states and emits events for detected changes.
func (w *Watcher) pollLoop(ctx context.Context, eventCh chan<- Event, chUsers *sync.WaitGroup) {
	defer w.wg.Done()
	defer chUsers.Done()

	w.debugLog("poll loop started", "interval", w.pollInterval)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	snapshot := make(map[string]fileState)

	// Take initial snapshot of all watched paths
	w.pollSnapshot(snapshot)

	for {
		select {
		case <-ctx.Done():
			w.debugLog("poll loop exiting: context cancelled")

			return

		case <-w.done:
			w.debugLog("poll loop exiting: watcher closed")

			return

		case <-ticker.C:
			w.pollDetectChanges(ctx, snapshot, eventCh)
		}
	}
}

// pollSnapshot takes a snapshot of all currently watched directories.
func (w *Watcher) pollSnapshot(snapshot map[string]fileState) {
	for _, rootPath := range w.copyWatchList() {
		w.pollWalkDir(rootPath, snapshot)
	}
}

//nolint:nilerr
func (w *Watcher) pollWalkDir(rootPath string, snapshot map[string]fileState) {
	_ = filepath.WalkDir(
		rootPath,
		func(path string, d os.DirEntry, err error) error { //nolint:nilerr,erraudit,varnamelen // poll loop tolerates unreadable dirs; the next poll retries
			if err != nil {
				return nil
			}

			if d.IsDir() {
				if w.shouldSkipDir(d.Name()) ||
					w.shouldExcludePath(path) {
					return filepath.SkipDir
				}

				// Load .gitignore for this directory before checking, mirroring walkDirFunc.
				w.loadGitignoreForDir(path)

				if w.shouldSkipByGitignore(path) {
					return filepath.SkipDir
				}
			}

			info, statErr := d.Info()
			if statErr != nil {
				return nil //nolint:nilerr
			}

			snapshot[w.pathKey(path)] = fileState{
				path:    path,
				modTime: info.ModTime(),
				size:    info.Size(),
				isDir:   d.IsDir(),
			}

			return nil
		},
	)
}

// pollDetectChanges compares the current filesystem state against the snapshot
// and emits events for any detected changes.
func (w *Watcher) pollDetectChanges(
	ctx context.Context,
	snapshot map[string]fileState,
	eventCh chan<- Event,
) {
	current := make(map[string]fileState)

	for _, rootPath := range w.copyWatchList() {
		w.pollWalkDir(rootPath, current)
	}

	// Detect new and modified files
	for key, curState := range current {
		prevState, existed := snapshot[key]

		if !existed {
			w.pollEmitEvent(ctx, Create, curState.path, curState, eventCh)

			continue
		}

		if !curState.isDir &&
			(curState.modTime != prevState.modTime || curState.size != prevState.size) {
			w.pollEmitEvent(ctx, Write, curState.path, curState, eventCh)
		}
	}

	// Detect removed files
	for key, prevState := range snapshot {
		if _, exists := current[key]; !exists {
			w.pollEmitEvent(ctx, Remove, prevState.path, fileState{
				path:    prevState.path,
				modTime: time.Time{},
				size:    0,
				isDir:   prevState.isDir,
			}, eventCh)
		}
	}

	// Replace snapshot with current state
	clear(snapshot)
	maps.Copy(snapshot, current)
}

// pollEmitEvent creates and emits an event from the polling goroutine,
// respecting filters and middleware.
//
//nolint:varnamelen
func (w *Watcher) pollEmitEvent(
	ctx context.Context,
	op Op,
	path string,
	state fileState,
	eventCh chan<- Event,
) {
	event := Event{
		Path:      path,
		Op:        op,
		Timestamp: time.Now(),
		IsDir:     state.isDir,
		Size:      state.size,
		ModTime:   state.modTime,
		Hash:      "",
	}

	w.debugLog("poll detected change", "op", op.String(), "path", path)

	if !w.passesFilters(event) {
		w.eventsFilteredOut.Add(1)

		return
	}

	w.emitEvent(ctx, event, eventCh)
}
