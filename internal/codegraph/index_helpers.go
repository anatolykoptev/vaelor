package codegraph

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/langutil"
)

// checkCache returns existing fresh meta, nil+nil when rebuild is needed, or
// nil+err on a hard failure (e.g. drop failed).
func checkCache(ctx context.Context, store *Store, repoKey, gname string) (*GraphMeta, error) {
	existing, err := getMeta(ctx, store, repoKey)
	if err != nil {
		return nil, fmt.Errorf("check cache: %w", err)
	}
	if existing == nil {
		return nil, nil
	}
	if isFresh(existing.BuiltAt, existing.TTLSeconds) {
		return existing, nil
	}
	// Snapshot the stale graph before dropping it.
	SnapshotBeforeRebuild(ctx, store, repoKey, gname)
	if dropErr := store.DropGraph(ctx, gname, repoKey); dropErr != nil {
		return nil, fmt.Errorf("drop stale graph: %w", dropErr)
	}
	return nil, nil
}

// relPath returns the path of abs relative to root.
func relPath(abs, root string) string {
	return langutil.RelPath(abs, root)
}
