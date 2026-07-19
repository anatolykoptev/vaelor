package filewatcher

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// fileState holds the metadata snapshot for a single file/directory.
type fileState struct {
	modTime time.Time
	size    int64
	isDir   bool
}

// pollLoop runs a periodic filesystem poll to detect changes that fsnotify
// may miss (NFS, FUSE, Docker volumes, etc.). It maintains a snapshot of
// file states and emits events for detected changes.
func (w *Watcher) pollLoop(ctx context.Context, eventCh chan<- Event) {
	defer w.wg.Done()

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
	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error { //nolint:nilerr,varnamelen
		if err != nil {
			return nil
		}

		if d.IsDir() && w.shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil //nolint:nilerr
		}

		snapshot[path] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
			isDir:   d.IsDir(),
		}

		return nil
	})
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
	for path, curState := range current {
		prevState, existed := snapshot[path]

		if !existed {
			w.pollEmitEvent(ctx, Create, path, curState, eventCh)

			continue
		}

		if !curState.isDir &&
			(curState.modTime != prevState.modTime || curState.size != prevState.size) {
			w.pollEmitEvent(ctx, Write, path, curState, eventCh)
		}
	}

	// Detect removed files
	for path := range snapshot {
		if _, exists := current[path]; !exists {
			prevState := snapshot[path]

			w.pollEmitEvent(ctx, Remove, path, fileState{
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
