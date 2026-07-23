package codegraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUpsertMeta_AppliesAgeSetup is a regression test for the bug where
// code_graph_meta accessors acquired a raw pool connection WITHOUT running
// ageSetup. code_graph_meta lives in the ag_catalog schema, but the gocode_app
// role's default search_path is `"$user", public` (no ag_catalog), so the
// unqualified upsert failed with 42P01 (undefined_table) and broke every graph
// rebuild. acquireAGE now applies ageSetup before touching the table.
//
// RED before the fix: upsertMeta -> 42P01 (relation "code_graph_meta" does not
// exist). GREEN after: the row round-trips.
//
// NOTE: setup uses a SEPARATE single connection (pgx.Connect), not the Store's
// pool, so the ag_catalog search_path is not leaked into a pooled connection
// that upsertMeta might reuse — otherwise the test masks the very bug it guards.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestUpsertMeta_AppliesAgeSetup(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	const testKey = "code_meta_agesetup_regression_test"

	// Ensure code_graph_meta exists in ag_catalog (matches prod layout), on a
	// dedicated connection that is closed immediately — never returned to a pool.
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	if _, err := setup.Exec(ctx, ageSetup); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("setup ageSetup: %v", err)
	}
	if _, err := setup.Exec(ctx, metaTableSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("ensure meta table: %v", err)
	}
	if _, err := setup.Exec(ctx, metaTableMigrateSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("migrate meta table: %v", err)
	}
	_, _ = setup.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
	_ = setup.Close(ctx)

	// Store pool — connections inherit the role's DEFAULT search_path (no
	// ag_catalog). This is the runtime condition under which the bug manifested.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	defer func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
	}()

	m := &GraphMeta{
		RepoKey:     testKey,
		RepoPath:    "/tmp/regression",
		GraphName:   testKey,
		FileCount:   1,
		SymbolCount: 2,
		EdgeCount:   3,
		BuiltAt:     time.Now().UTC(),
		TTLSeconds:  3600,
		ContentHash: "abc123def456",
	}

	// Decisive assertion: upsertMeta must succeed even though the pool's default
	// search_path excludes ag_catalog. Without acquireAGE this returns 42P01.
	if err := upsertMeta(ctx, store, m); err != nil {
		t.Fatalf("upsertMeta failed (acquireAGE not applied?): %v", err)
	}

	got, err := getMeta(ctx, store, testKey)
	if err != nil {
		t.Fatalf("getMeta: %v", err)
	}
	if got == nil {
		t.Fatal("getMeta returned nil after a successful upsert")
	}
	if got.RepoKey != testKey || got.GraphName != testKey || got.EdgeCount != 3 {
		t.Fatalf("getMeta round-trip mismatch: %+v", got)
	}
	if got.ContentHash != "abc123def456" {
		t.Fatalf("content_hash round-trip mismatch: got %q, want %q", got.ContentHash, "abc123def456")
	}
}
