package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// oldMetaTableSQL is the PRE-#592 schema for code_graph_meta — no
// content_hash column. Used to reproduce a prod deployment that created the
// table before the content_hash migration, so the SELECT in getMeta/ListMeta
// (which now references content_hash) hits 42703 (undefined_column) at PLAN
// time.
const oldMetaTableSQL = `
CREATE TABLE IF NOT EXISTS code_graph_meta (
    repo_key      TEXT PRIMARY KEY,
    repo_path     TEXT NOT NULL,
    graph_name    TEXT NOT NULL,
    file_count    INT,
    symbol_count  INT,
    edge_count    INT,
    built_at      TIMESTAMPTZ NOT NULL,
    ttl_seconds   INT DEFAULT 3600
)`

// TestGetMeta_OldSchema_ReturnsNilNoError is the falsification test for the
// #592 review BLOCKER fix. On a prod code_graph_meta created by the OLD schema
// (no content_hash column), getMeta's SELECT … content_hash fails at PLAN
// time with 42703 (undefined_column). Without 42703 tolerance, getMeta
// errors, IndexRepo returns before EnsureGraph ever runs the ALTER,
// migration is unreachable, and every AGE-graph tool is permanently broken
// post-deploy. With the fix, getMeta treats 42703 as a cache miss (nil, nil)
// → IndexRepo falls through to EnsureGraph → ALTER runs → self-heals.
//
// RED before the fix: getMeta surfaces the 42703 error → t.Fatalf. GREEN
// after: getMeta returns (nil, nil).
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestGetMeta_OldSchema_ReturnsNilNoError(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	const testKey = "code_meta_old_schema_get_test"

	// Recreate the OLD-schema table (no content_hash) and seed a row.
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	if _, err := setup.Exec(ctx, ageSetup); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("setup ageSetup: %v", err)
	}
	// Drop the current table and recreate with the OLD schema so the SELECT
	// hits 42703 (not a pre-existing content_hash column).
	if _, err := setup.Exec(ctx, "DROP TABLE IF EXISTS code_graph_meta"); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("drop meta table: %v", err)
	}
	if _, err := setup.Exec(ctx, oldMetaTableSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("create old-schema meta table: %v", err)
	}
	if _, err := setup.Exec(ctx,
		"INSERT INTO code_graph_meta (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds) VALUES ($1, '/tmp/old', $2, 1, 1, 0, $3, 3600)",
		testKey, testKey, time.Now().UTC(),
	); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("seed old-schema row: %v", err)
	}
	_ = setup.Close(ctx)

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DROP TABLE IF EXISTS code_graph_meta")
		_, _ = c.Exec(ctx, metaTableSQL)
		_, _ = c.Exec(ctx, metaTableMigrateSQL)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	got, err := getMeta(ctx, store, testKey)
	if err != nil {
		t.Fatalf("getMeta on old-schema table: want nil error (42703 tolerated as cache miss), got: %v", err)
	}
	if got != nil {
		t.Errorf("getMeta on old-schema table: want nil meta (cache miss), got %+v — 42703 must be treated as a cache miss so EnsureGraph can run the ALTER", got)
	}
}

// TestListMeta_OldSchema_ReturnsEmptyNoError is the ListMeta half of the
// BLOCKER falsification: ListMeta's SELECT also references content_hash, so
// the same 42703 applies. Without tolerance, the boot-warm goroutine
// (publishCodeGraphAgeGauge) would error on every fresh deploy.
//
// RED before the fix: ListMeta surfaces the 42703 error → t.Fatalf.
// GREEN after: ListMeta returns (nil, nil).
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestListMeta_OldSchema_ReturnsEmptyNoError(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	// Recreate the OLD-schema table (no content_hash) and seed a row.
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	if _, err := setup.Exec(ctx, ageSetup); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("setup ageSetup: %v", err)
	}
	if _, err := setup.Exec(ctx, "DROP TABLE IF EXISTS code_graph_meta"); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("drop meta table: %v", err)
	}
	if _, err := setup.Exec(ctx, oldMetaTableSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("create old-schema meta table: %v", err)
	}
	if _, err := setup.Exec(ctx,
		"INSERT INTO code_graph_meta (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds) VALUES ($1, '/tmp/old', $1, 1, 1, 0, $2, 3600)",
		"code_meta_old_schema_list_test", time.Now().UTC(),
	); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("seed old-schema row: %v", err)
	}
	_ = setup.Close(ctx)

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DROP TABLE IF EXISTS code_graph_meta")
		_, _ = c.Exec(ctx, metaTableSQL)
		_, _ = c.Exec(ctx, metaTableMigrateSQL)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	metas, err := ListMeta(ctx, store)
	if err != nil {
		t.Fatalf("ListMeta on old-schema table: want nil error (42703 tolerated), got: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("ListMeta on old-schema table: want 0 rows (cache miss), got %d", len(metas))
	}
}

// TestCheckCache_StaleContentHash_FallsThroughToRebuild verifies that
// checkCache does NOT return a cached meta when the stored content_hash is
// stale (within TTL but hash mismatch) — it falls through to the rebuild path
// (returns nil, nil). This is the #592 core behaviour: a stale graph must not
// be served just because the TTL hasn't expired.
//
// RED if the content-hash check is removed/reversed: checkCache would return
// the cached meta → test fails on "want nil, got non-nil".
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestCheckCache_StaleContentHash_FallsThroughToRebuild(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	const testKey = "code_checkcache_stale_hash_test"

	// Build a real temp repo so RepoContentHash has something to hash.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	// Full schema setup + create the AGE graph so DropGraph (on the stale
	// path) succeeds.
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

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
		_, _ = c.Exec(ctx, "SELECT ag_catalog.drop_graph($1, true)", testKey)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	// Create the AGE graph so the stale path's DropGraph succeeds.
	if err := store.EnsureGraph(ctx, testKey); err != nil {
		t.Fatalf("EnsureGraph: %v", err)
	}

	// Seed a FRESH (within TTL) meta row with a deliberately stale hash.
	if err := upsertMeta(ctx, store, &GraphMeta{
		RepoKey: testKey, RepoPath: root, GraphName: testKey,
		FileCount: 1, SymbolCount: 1, EdgeCount: 0,
		BuiltAt: time.Now().UTC(), TTLSeconds: 3600,
		ContentHash: "stale-hash-that-does-not-match-anything",
	}); err != nil {
		t.Fatalf("upsertMeta stale-hash: %v", err)
	}

	got, err := checkCache(ctx, store, testKey, testKey, root)
	if err != nil {
		t.Fatalf("checkCache stale-hash: want nil error, got: %v", err)
	}
	if got != nil {
		t.Errorf("checkCache stale-hash: want nil (fall through to rebuild), got %+v — a stale content_hash within TTL must trigger a rebuild, not serve the cached graph", got)
	}
}

// TestCheckCache_EmptyContentHash_TemporalOnly verifies that a meta row with
// an empty content_hash (pre-migration row that EnsureGraph just ALTERed but
// hasn't rebuilt yet) is served on a temporal-TTL hit — preserving backward
// compatibility with graphs built before the content_hash column was added.
//
// RED if the empty-hash branch is removed: checkCache would compute
// RepoContentHash and compare against "" → mismatch → rebuild, returning nil
// instead of the cached meta.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestCheckCache_EmptyContentHash_TemporalOnly(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	const testKey = "code_checkcache_empty_hash_test"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

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

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	// Seed a FRESH meta row with an EMPTY content_hash (pre-migration row).
	if err := upsertMeta(ctx, store, &GraphMeta{
		RepoKey: testKey, RepoPath: root, GraphName: testKey,
		FileCount: 1, SymbolCount: 1, EdgeCount: 0,
		BuiltAt: time.Now().UTC(), TTLSeconds: 3600,
		ContentHash: "",
	}); err != nil {
		t.Fatalf("upsertMeta empty-hash: %v", err)
	}

	got, err := checkCache(ctx, store, testKey, testKey, root)
	if err != nil {
		t.Fatalf("checkCache empty-hash: want nil error, got: %v", err)
	}
	if got == nil {
		t.Fatal("checkCache empty-hash: want cached meta (temporal-only), got nil — an empty content_hash must fall back to temporal TTL, not force a rebuild")
	}
	if got.RepoKey != testKey {
		t.Errorf("checkCache empty-hash: repo_key = %q, want %q", got.RepoKey, testKey)
	}
}
