package codegraph

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/langutil"
)

// checkCache returns existing fresh meta, nil+nil when rebuild is needed, or
// nil+err on a hard failure (e.g. drop failed).
//
// A cached graph is considered fresh ONLY if both conditions hold:
//   - BuiltAt is within TTLSeconds of now (temporal freshness)
//   - content_hash matches ingest.RepoContentHash(root) (content freshness)
//
// The content-hash check (#592) prevents serving a stale graph when source
// files have been modified within the TTL window. If the stored content_hash
// is empty (pre-migration row), only the temporal check applies — preserving
// backward compatibility with graphs built before the content_hash column
// was added.
func checkCache(ctx context.Context, store *Store, repoKey, gname, root string) (*GraphMeta, error) {
	existing, err := getMeta(ctx, store, repoKey)
	if err != nil {
		return nil, fmt.Errorf("check cache: %w", err)
	}
	if existing == nil {
		return nil, nil
	}
	if isFresh(existing.BuiltAt, existing.TTLSeconds) {
		// Temporal TTL hit — validate content hash to detect file changes
		// within the TTL window (#592).
		if existing.ContentHash != "" {
			currentHash := ingest.RepoContentHash(root)
			if currentHash != existing.ContentHash {
				slog.Info("codegraph: cache hit but content hash mismatch — rebuilding",
					slog.String("repo", root),
					slog.String("stored_hash", existing.ContentHash[:min(12, len(existing.ContentHash))]),
					slog.String("current_hash", currentHash[:min(12, len(currentHash))]))
				// Fall through to stale path: snapshot + drop + rebuild.
			} else {
				return existing, nil
			}
		} else {
			// Pre-migration row (no content_hash) — temporal check only.
			return existing, nil
		}
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
