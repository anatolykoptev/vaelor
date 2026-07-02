//go:build !nointegration
// +build !nointegration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestPublishCodeGraphAgeGauge_SeedsFromStoredMeta is the end-to-end
// regression guard for the gocode_code_graph_age_seconds boot-warm fix
// (2026-07-01 metrics audit): on a fresh process start, no build cycle has
// run yet, so the ONLY source of truth for a repo's last-build age is the
// persisted code_graph_meta row. publishCodeGraphAgeGauge must read that row
// (via ListMeta) and seed the gauge with the real age — not wait for the
// next build to complete, which is exactly the window GocodeCodeGraphStale
// went dark in on every deploy.
//
// RED before the fix: publishCodeGraphAgeGauge does not exist / does not
// call ListMeta, so codeGraphAgeSeconds never gets set for a repo whose
// build predates this process, and the >= assertion fails.
//
// Skipped unless DATABASE_URL is configured (uses the real EnsureGraph
// schema-provisioning path — no ad-hoc DDL duplicated here).
func TestPublishCodeGraphAgeGauge_SeedsFromStoredMeta(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping code_graph age-gauge integration test")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	// t.Cleanup callbacks run LIFO, AFTER the test function (and any `defer`
	// inside it) has already returned — a `defer pool.Close()` here would
	// close the pool BEFORE the DB-cleanup t.Cleanup below runs, and every
	// pool.Exec inside it would fail with "closed pool" (exactly what broke
	// CI: an ordering bug in the test's own teardown, not the production
	// code under test — that assertion had already passed). Registering
	// pool.Close as the FIRST t.Cleanup makes it run LAST.
	t.Cleanup(func() { pool.Close() })
	store := codegraph.NewStore(pool)

	const repoKey = "__test_publish_code_graph_age_gauge__"

	// EnsureGraph is the real production schema-provisioning path (used by
	// IndexRepo before every build) — reusing it means this test never
	// duplicates the code_graph_meta DDL.
	if err := store.EnsureGraph(ctx, repoKey); err != nil {
		t.Fatalf("EnsureGraph: %v", err)
	}

	setSearchPath := func() {
		if _, spErr := pool.Exec(ctx, `SET search_path = ag_catalog, "$user", public`); spErr != nil {
			t.Fatalf("set search_path: %v", spErr)
		}
	}
	cleanup := func() {
		setSearchPath()
		_, _ = pool.Exec(ctx, `DELETE FROM code_graph_meta WHERE repo_key = $1`, repoKey)
	}
	cleanup()
	// Registered SECOND -> runs FIRST (LIFO), while the pool is still open.
	t.Cleanup(func() {
		cleanup()
		_ = store.DropGraph(ctx, repoKey, repoKey)
	})

	// builtAt is 2h in the past — well below the 26h GocodeCodeGraphStale
	// threshold, but large enough to distinguish "seeded from stored value"
	// from "seeded at 0" (the exact fake-freshness failure mode this fix
	// must avoid).
	builtAt := time.Now().Add(-2 * time.Hour).UTC()
	setSearchPath()
	if _, err := pool.Exec(ctx, `
		INSERT INTO code_graph_meta
		    (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds)
		VALUES ($1, $2, $3, 1, 1, 0, $4, 3600)
		ON CONFLICT (repo_key) DO UPDATE SET built_at = EXCLUDED.built_at`,
		repoKey, "/tmp/publish-gauge-test", repoKey, builtAt); err != nil {
		t.Fatalf("insert meta row: %v", err)
	}

	// nil scopeDirs = back-compat "publish everything" (AUTO_INDEX_DIRS
	// unset); this test's repo_path is arbitrary and irrelevant to scoping.
	publishCodeGraphAgeGauge(ctx, store, nil)

	got := testutil.ToFloat64(codeGraphAgeSeconds.WithLabelValues(repoKey))
	const wantMinSeconds = 2*3600 - 60 // ~2h, minus a minute of slack
	if got < wantMinSeconds {
		t.Errorf("codeGraphAgeSeconds{repo=%q} = %.0f, want >= %.0f (builtAt was 2h ago; a boot-warm "+
			"that seeds 0 instead of the real age would fail this)", repoKey, got, float64(wantMinSeconds))
	}
}
