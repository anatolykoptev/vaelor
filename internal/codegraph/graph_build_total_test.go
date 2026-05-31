package codegraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestGraphBuildTotal_ErrorPath exercises the "error" outcome of IndexRepo via a
// Store backed by an unreachable connection — no live DATABASE_URL required.
//
// When IndexRepo calls checkCache → getMeta → acquireAGE → pool.Acquire, the
// unreachable pool returns an error immediately.  Before CG-T6 wiring, the
// graphBuildTotal{status="error"} counter is never bumped; this test drives the
// RED→GREEN cycle.
func TestGraphBuildTotal_ErrorPath(t *testing.T) {
	t.Parallel()

	// Build a pool that will fail on every Acquire (port 1 is not listening).
	cfg, err := pgxpool.ParseConfig("postgres://testuser:testpass@localhost:1/nodb?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	repo := graphName("/test/error/path")

	c := graphBuildTotal.WithLabelValues(repo, "error")
	before := readCounter(t, c)

	_, indexErr := IndexRepo(context.Background(), store, "/test/error/path", false, IndexConfig{})
	if indexErr == nil {
		t.Fatal("IndexRepo with unreachable pool: expected error, got nil")
	}

	after := readCounter(t, c)
	if after-before != 1 {
		t.Errorf("graphBuildTotal{status=error}: want +1, got +%.0f (IndexRepo returned: %v)", after-before, indexErr)
	}
}

// TestGraphBuildTotal_SkipPath exercises the "skip" (cache-fresh) outcome of
// IndexRepo.  This test requires a live DATABASE_URL (PostgreSQL + AGE), so it
// is skipped when the variable is not set.
//
// The test:
//  1. Creates a real Store and calls EnsureGraph to populate code_graph_meta.
//  2. Directly upserts a fresh meta row (TTL not expired).
//  3. Calls IndexRepo — which hits checkCache, finds the fresh row, and returns
//     the cached GraphMeta without rebuilding.
//  4. Asserts graphBuildTotal{status="skip"} incremented by 1.
func TestGraphBuildTotal_SkipPath(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping DB-gated skip-path test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	root := "/test/skip/path"
	repo := graphName(root)

	// Seed a fresh meta row so checkCache returns it (TTL=3600 → fresh for 1h).
	meta := &GraphMeta{
		RepoKey:    repo,
		RepoPath:   root,
		GraphName:  repo,
		FileCount:  1,
		BuiltAt:    time.Now().UTC(),
		TTLSeconds: 3600,
	}
	if err := upsertMeta(ctx, store, meta); err != nil {
		t.Fatalf("upsertMeta: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup: remove the seeded meta row.
		conn, err := store.pool.Acquire(ctx)
		if err != nil {
			return
		}
		defer conn.Release()
		_, _ = conn.Exec(ctx, `DELETE FROM code_graph_meta WHERE repo_key = $1`, repo)
	})

	c := graphBuildTotal.WithLabelValues(repo, "skip")
	before := readCounter(t, c)

	_, indexErr := IndexRepo(ctx, store, root, false, IndexConfig{})
	if indexErr != nil {
		t.Fatalf("IndexRepo (cache-fresh): unexpected error: %v", indexErr)
	}

	after := readCounter(t, c)
	if after-before != 1 {
		t.Errorf("graphBuildTotal{status=skip}: want +1, got +%.0f", after-before)
	}
}
