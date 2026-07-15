package codegraph

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEnsureGraphConcurrency forces the Postgres CREATE TABLE IF NOT EXISTS
// race on the fixed codegraph bookkeeping tables. Eight goroutines call
// EnsureGraph on distinct graph names against one shared pool; with the
// provisioning sequence unprotected, at least one loses on the
// pg_type_typname_nsp_index unique index (SQLSTATE 23505).
//
// This test DROP TABLE ... CASCADEs the shared (non-graph-scoped) codegraph
// bookkeeping tables to force the race, so it is gated on PR_TEST_DATABASE_URL
// (NOT DATABASE_URL, the prod var) — same convention as
// internal/embeddings/graph_candidates_test.go's testPoolAGE/testPoolPR: a
// plain `go test ./internal/codegraph/...` with only prod DATABASE_URL set
// must be a no-op, not wipe all-repos metadata. preflight/nightly export
// PR_TEST_DATABASE_URL, so this still runs in CI.
func TestEnsureGraphConcurrency(t *testing.T) {
	dbURL := os.Getenv("PR_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("PR_TEST_DATABASE_URL not set — skipping EnsureGraph concurrency test")
	}

	ctx := context.Background()
	cfg, parseErr := pgxpool.ParseConfig(dbURL)
	if parseErr != nil {
		t.Fatalf("parse PR_TEST_DATABASE_URL: %v", parseErr)
	}
	if strings.EqualFold(cfg.ConnConfig.Database, "gocode") {
		t.Fatalf("refusing to run EnsureGraph concurrency test against the prod gocode DB; " +
			"set PR_TEST_DATABASE_URL to an isolated DB (e.g. gocode_testiso)")
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	// t.Cleanup callbacks run LIFO. Register pool.Close first so it runs last.
	t.Cleanup(func() { pool.Close() })

	store := NewStore(pool)

	const n = 8
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = fmt.Sprintf("code_conc_%02d", i)
	}

	// Drop any leftover graphs and the fixed provisioning tables so the
	// CREATE TABLE IF NOT EXISTS race on pg_type is forced in this run.
	for _, name := range names {
		_ = store.DropGraph(ctx, name, name)
	}
	for _, tbl := range []string{"code_graph_meta", "code_file_mtimes", "code_graph_snapshots", "code_dead_code_scores"} {
		_, _ = pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS ag_catalog.%s CASCADE", tbl))
	}

	t.Cleanup(func() {
		for _, name := range names {
			_ = store.DropGraph(ctx, name, name)
		}
	})

	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			errs[i] = store.EnsureGraph(ctx, names[i])
		}(i)
	}

	start.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("EnsureGraph(%s): %v", names[i], err)
		}
	}
}
