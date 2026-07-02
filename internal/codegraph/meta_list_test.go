package codegraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestListMeta_ReturnsAllRows is the regression guard for the
// gocode_code_graph_age_seconds boot-warm fix (2026-07-01 metrics audit):
// GocodeCodeGraphStale went dark on every restart because the gauge only
// existed for repos with a build cycle that completed AFTER the process
// started. publishCodeGraphAgeGauge (cmd/go-code) seeds the gauge from
// ListMeta at boot instead, so ListMeta must return every stored row —
// including one with an already-stale BuiltAt — not just the most recent.
//
// RED before the fix: ListMeta does not exist (compile error) / an
// incomplete implementation that only returns one row or drops the stale
// one fails the "both keys present" and "stale BuiltAt round-trips" assertions.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestListMeta_ReturnsAllRows(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	const (
		freshKey = "code_meta_list_test_fresh"
		staleKey = "code_meta_list_test_stale"
	)

	// Ensure code_graph_meta exists in ag_catalog (matches prod layout), on a
	// dedicated connection closed immediately — never returned to a pool, so
	// the ag_catalog search_path never leaks into a pooled connection (same
	// discipline as TestUpsertMeta_AppliesAgeSetup).
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
	_, _ = setup.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = ANY($1)",
		[]string{freshKey, staleKey})
	_ = setup.Close(ctx)

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = ANY($1)",
			[]string{freshKey, staleKey})
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	freshBuiltAt := time.Now().Add(-time.Minute).UTC()
	// staleBuiltAt is well past the 93600s GocodeCodeGraphStale threshold —
	// exactly the "already stale before this process started" case the
	// boot-warm fix must surface, not hide.
	staleBuiltAt := time.Now().Add(-30 * time.Hour).UTC()

	if err := upsertMeta(ctx, store, &GraphMeta{
		RepoKey: freshKey, RepoPath: "/tmp/fresh", GraphName: freshKey,
		FileCount: 1, SymbolCount: 1, EdgeCount: 0,
		BuiltAt: freshBuiltAt, TTLSeconds: 3600,
	}); err != nil {
		t.Fatalf("upsertMeta fresh: %v", err)
	}
	if err := upsertMeta(ctx, store, &GraphMeta{
		RepoKey: staleKey, RepoPath: "/tmp/stale", GraphName: staleKey,
		FileCount: 1, SymbolCount: 1, EdgeCount: 0,
		BuiltAt: staleBuiltAt, TTLSeconds: 3600,
	}); err != nil {
		t.Fatalf("upsertMeta stale: %v", err)
	}

	metas, err := ListMeta(ctx, store)
	if err != nil {
		t.Fatalf("ListMeta: %v", err)
	}

	byKey := make(map[string]GraphMeta, len(metas))
	for _, m := range metas {
		byKey[m.RepoKey] = m
	}

	fresh, ok := byKey[freshKey]
	if !ok {
		t.Fatalf("ListMeta: fresh repo_key %q not found in %d rows", freshKey, len(metas))
	}
	if age := time.Since(fresh.BuiltAt); age < 0 || age > 5*time.Minute {
		t.Errorf("fresh BuiltAt round-trip: age = %v, want within [0, 5m]", age)
	}

	stale, ok := byKey[staleKey]
	if !ok {
		t.Fatalf("ListMeta: stale repo_key %q not found in %d rows — a boot-warm that drops "+
			"already-stale rows would defeat GocodeCodeGraphStale exactly for the repos it must catch",
			staleKey, len(metas))
	}
	if age := time.Since(stale.BuiltAt); age < 25*time.Hour {
		t.Errorf("stale BuiltAt round-trip: age = %v, want > 25h (GocodeCodeGraphStale threshold is 26h)", age)
	}
}

// TestListMeta_NoTable_ReturnsEmptyNoError asserts that ListMeta treats a
// missing code_graph_meta table (42P01) as "no repos known yet" — matching
// getMeta's cold-path handling — rather than surfacing an error that would
// make the boot-warm goroutine noisy on every fresh deploy.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestListMeta_NoTable_ReturnsEmptyNoError(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	// Drop the table (if present) so acquireAGE + the SELECT hit 42P01 —
	// reproducing a genuinely fresh database.
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	if _, err := setup.Exec(ctx, ageSetup); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("setup ageSetup: %v", err)
	}
	_, dropErr := setup.Exec(ctx, "DROP TABLE IF EXISTS code_graph_meta")
	_ = setup.Close(ctx)
	if dropErr != nil {
		t.Fatalf("drop meta table: %v", dropErr)
	}

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, metaTableSQL)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	metas, err := ListMeta(ctx, store)
	if err != nil {
		t.Fatalf("ListMeta on missing table: want nil error (cold-path), got: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("ListMeta on missing table: want 0 rows, got %d", len(metas))
	}
}
