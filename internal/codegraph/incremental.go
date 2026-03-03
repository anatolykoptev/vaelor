package codegraph

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

// computeChangedFiles compares current file mtimes with stored ones.
// Returns: files to re-parse (changed or new), paths to delete (removed files).
func computeChangedFiles(current []*ingest.File, stored map[string]time.Time) (changed []*ingest.File, removed []string) {
	currentPaths := make(map[string]bool, len(current))
	for _, f := range current {
		currentPaths[f.RelPath] = true
		storedTime, exists := stored[f.RelPath]
		if !exists || !f.ModTime.Equal(storedTime) {
			changed = append(changed, f)
		}
	}
	for path := range stored {
		if !currentPaths[path] {
			removed = append(removed, path)
		}
	}
	return changed, removed
}

// storeFileMtimes saves file mtimes for future incremental delta computation.
func storeFileMtimes(ctx context.Context, store *Store, repoKey string, files []*ingest.File) {
	mtimes := make(map[string]time.Time, len(files))
	for _, f := range files {
		mtimes[f.RelPath] = f.ModTime
	}
	if err := store.UpsertFileMtimes(ctx, repoKey, mtimes); err != nil {
		slog.Warn("codegraph: store mtimes failed", slog.Any("error", err))
	}
}
