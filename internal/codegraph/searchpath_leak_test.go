package codegraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dto "github.com/prometheus/client_model/go"
)

// testPoolMaxConns1 builds a pgxpool with MaxConns=1 and the AfterRelease hook that
// SR-A wires in register.go. MaxConns=1 forces dirty-conn reuse: after an
// acquireAGE call the single connection is returned to the pool and immediately
// re-acquired by the next caller — if the hook does not clean the conn, the
// data-path statement runs inside the ag_catalog search_path set by acquireAGE.
//
// The hook uses DISCARD ALL (matches register.go) — this resets search_path,
// session GUCs (synchronous_commit, statement_timeout), temp objects, and
// prepared statements in one command.
func testPoolMaxConns1(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = 1
	// Wire the same AfterRelease hook that register.go installs in production.
	// DISCARD ALL resets: search_path, all session-level GUCs (synchronous_commit,
	// statement_timeout, etc.), temp tables, and server-side prepared statements.
	// Safe here: the pool uses pgx default exec mode (simple protocol) — no
	// server-side prepared statements survive across Release boundaries anyway.
	cfg.AfterRelease = func(conn *pgx.Conn) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, "DISCARD ALL"); err != nil {
			return false
		}
		return true
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestSearchPathLeak_MaxConns1_RowLandsInPublic is the load-bearing correctness
// test for SR-A.
//
// Setup: MaxConns=1 forces the pool's single physical connection to be reused
// across acquire/release cycles. The test dirties the connection with an AGE
// search_path via acquireAGE, releases it (triggering AfterRelease → DISCARD ALL),
// then issues a BARE (unqualified) INSERT INTO code_repo_state. With the hook the
// bare name resolves to public.code_repo_state; without it the name resolves to
// ag_catalog.code_repo_state (the AGE-dirty search_path wins).
//
// Falsification (red-on-revert): stash the AfterRelease body (replace with
// `return true` — no DISCARD ALL), run the test, confirm it fails at assertion B
// with "row leaked into ag_catalog.code_repo_state". Restore → GREEN.
// Real evidence captured during PR #173 review: see commit message.
func TestSearchPathLeak_MaxConns1_RowLandsInPublic(t *testing.T) {
	pool := testPoolMaxConns1(t)
	ctx := context.Background()
	store := NewStore(pool)

	const repoKey = "searchpath-leak-test/maxconns1"

	// Ensure public.code_repo_state exists. Schema-qualified DDL is safe here —
	// we are creating the table, not testing routing of bare names.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS public.code_repo_state (
			repo_key   TEXT PRIMARY KEY,
			head_sha   TEXT NOT NULL,
			indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("ensure public.code_repo_state: %v", err)
	}

	// Also ensure ag_catalog.code_repo_state exists so the bare-name INSERT has
	// somewhere to land when the hook is absent (falsification prerequisite).
	_, createAGErr := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ag_catalog.code_repo_state (
			repo_key   TEXT PRIMARY KEY,
			head_sha   TEXT NOT NULL,
			indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if createAGErr != nil {
		t.Skipf("cannot create fixture table in ag_catalog (privilege): %v", createAGErr)
	}

	t.Cleanup(func() {
		bg := context.Background()
		// Remove fixture rows from both schemas so no cross-run pollution.
		_, _ = pool.Exec(bg, "DELETE FROM public.code_repo_state WHERE repo_key=$1", repoKey)
		_, _ = pool.Exec(bg, "DELETE FROM ag_catalog.code_repo_state WHERE repo_key=$1", repoKey)
	})

	// Step 1: dirty the single connection with AGE search_path.
	// acquireAGE runs SET search_path TO ag_catalog, "$user", public.
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Skipf("AGE not available: %v", err)
	}
	// Release triggers AfterRelease → DISCARD ALL → search_path reset to default.
	conn.Release()

	// Step 2: bare (unqualified) INSERT into code_repo_state.
	// This is the critical probe: with a clean search_path the name resolves to
	// public.code_repo_state. Without the hook (AGE-dirty path still active) it
	// resolves to ag_catalog.code_repo_state — and assertion B below fails.
	const sha = "deadbeef"
	_, insertErr := pool.Exec(ctx,
		`INSERT INTO code_repo_state (repo_key, head_sha, indexed_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (repo_key) DO UPDATE SET head_sha = EXCLUDED.head_sha, indexed_at = NOW()`,
		repoKey, sha)
	if insertErr != nil {
		t.Fatalf("bare INSERT into code_repo_state: %v", insertErr)
	}

	// Assertion A: row exists in public.code_repo_state (schema-qualified read —
	// not testing routing here, just verifying the row landed where expected).
	var gotSHA string
	err = pool.QueryRow(ctx,
		"SELECT head_sha FROM public.code_repo_state WHERE repo_key=$1", repoKey).
		Scan(&gotSHA)
	if err != nil {
		t.Errorf("row not found in public.code_repo_state: %v", err)
	} else if gotSHA != sha {
		t.Errorf("public.code_repo_state.head_sha = %q, want %q", gotSHA, sha)
	}

	// Assertion B: row NOT in ag_catalog.code_repo_state.
	// This is the falsification gate: remove AfterRelease and the bare INSERT in
	// Step 2 routes to ag_catalog (dirty search_path wins) → this assertion fails.
	var leaked bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM ag_catalog.code_repo_state WHERE repo_key=$1)",
		repoKey).Scan(&leaked)
	if err != nil {
		t.Errorf("ag_catalog.code_repo_state probe failed: %v", err)
	}
	if leaked {
		t.Errorf("row leaked into ag_catalog.code_repo_state — AfterRelease DISCARD ALL hook is not working")
	}
}

// TestSchemaDriftAssertion_CounterBumped verifies the SR-OBS drift guard:
// seeding a row in ag_catalog.code_repo_state (simulated via a direct INSERT
// into a fixture table) causes AssertSchemaDrift to bump gocode_schema_drift_total.
//
// The test drives the assertion through the real AssertSchemaDrift function
// (not a manual counter.Inc()), so it fails if AssertSchemaDrift stops looking
// at ag_catalog.
func TestSchemaDriftAssertion_CounterBumped(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()

	// Check if ag_catalog exists (requires AGE to be installed).
	var agCatalogExists bool
	_ = pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'ag_catalog')
	`).Scan(&agCatalogExists)
	if !agCatalogExists {
		t.Skip("ag_catalog schema not present — AGE not installed, drift guard untestable")
	}

	// Create a temporary code_repo_state table in ag_catalog as the drift fixture.
	// We need CREATE privilege on ag_catalog for this — if not available, skip.
	_, createErr := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ag_catalog.code_repo_state (
			repo_key   TEXT PRIMARY KEY,
			head_sha   TEXT NOT NULL,
			indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if createErr != nil {
		t.Skipf("cannot create fixture table in ag_catalog (privilege): %v", createErr)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS ag_catalog.code_repo_state")
	})

	// The fixture table now exists in ag_catalog — this is the "drifted" state.
	store := NewStore(pool)

	// Read the counter before calling AssertSchemaDrift.
	c := schemaDriftTotal.WithLabelValues("code_repo_state")
	before := readCounterValue(t, c)

	store.AssertSchemaDrift(ctx)

	after := readCounterValue(t, c)
	if after-before < 1 {
		t.Errorf("gocode_schema_drift_total{table=code_repo_state}: want +1, got +%.0f", after-before)
	}
}

// readCounterValue reads a prometheus.Counter's current value.
// Duplicates route_metrics_test.go's readCounter helper to keep this file
// self-contained (package-level isolation).
func readCounterValue(t *testing.T, c interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("counter.Write: %v", err)
	}
	return m.GetCounter().GetValue()
}
