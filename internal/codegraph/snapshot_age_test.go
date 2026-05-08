package codegraph

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestBuildSnapshotFromAGE_GraphMissing_ReturnsEmptySnapshot verifies that
// buildSnapshotFromAGE returns (Snapshot{}, nil) instead of propagating the
// error when the AGE graph does not exist. This exercises the
// IsGraphMissingError guard added to the write-path callers.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestBuildSnapshotFromAGE_GraphMissing_ReturnsEmptySnapshot(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)

	// Use a graph name that does not exist — guaranteed by using a unique name
	// that we never create. We clean up defensively in case a previous run left it.
	const testGraph = "code_snap_missing_test"
	_ = store.DropGraph(ctx, testGraph, testGraph) // ignore error (may not exist)

	snap, err := buildSnapshotFromAGE(ctx, store, testGraph)

	if err != nil {
		t.Errorf("expected nil error for missing graph, got: %v", err)
	}
	if len(snap.Symbols) != 0 {
		t.Errorf("expected empty Symbols for missing graph, got %d", len(snap.Symbols))
	}
	if len(snap.Edges) != 0 {
		t.Errorf("expected empty Edges for missing graph, got %d", len(snap.Edges))
	}
}
