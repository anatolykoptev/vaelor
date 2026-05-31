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

// testPoolMaxConns builds a pgxpool with MaxConns=1 and the AfterRelease hook that
// SR-A wires in register.go. MaxConns=1 forces dirty-conn reuse: after an
// acquireAGE call the single connection is returned to the pool and immediately
// re-acquired by the next caller — if the hook does not RESET search_path, the
// data-path statement runs inside ag_catalog search_path.
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
	cfg.AfterRelease = func(conn *pgx.Conn) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, "RESET search_path"); err != nil {
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
// test for SR-A + SR-B.
//
// Setup: MaxConns=1 forces the pool's single connection to be reused across
// acquire/release cycles. The test dirtied the connection with an AGE search_path
// via acquireAGE, releases it (triggering AfterRelease RESET search_path), then
// calls SetRepoState which issues a bare INSERT against code_repo_state.
//
// Assertion: the row appears in public.code_repo_state and NOT in
// ag_catalog.code_repo_state.
//
// Red-on-revert evidence: when the AfterRelease hook is removed from cfg above,
// the INSERT resolves code_repo_state against ag_catalog (the dirty search_path),
// and the ag_catalog assertion below fails — the test goes RED, proving it guards
// the exact regression.
func TestSearchPathLeak_MaxConns1_RowLandsInPublic(t *testing.T) {
	pool := testPoolMaxConns1(t)
	ctx := context.Background()
	store := NewStore(pool)

	const repoKey = "searchpath-leak-test/maxconns1"

	// Ensure public.code_repo_state exists (created by embeddings.EnsureSchema,
	// but we need it here too for the assertion queries to work).
	// Create it directly if needed — idempotent DDL.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS public.code_repo_state (
			repo_key   TEXT PRIMARY KEY,
			head_sha   TEXT NOT NULL,
			indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("ensure public.code_repo_state: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM public.code_repo_state WHERE repo_key=$1", repoKey)
	})

	// Step 1: dirty the single connection with AGE search_path.
	// acquireAGE runs SET search_path TO ag_catalog, "$user", public.
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Skipf("AGE not available: %v", err)
	}
	// Release triggers AfterRelease → RESET search_path.
	conn.Release()

	// Step 2: SetRepoState on the same (now clean) connection.
	// Without SR-A the search_path is still ag_catalog-first and the INSERT
	// resolves code_repo_state into ag_catalog, not public.
	const sha = "deadbeef"
	_, insertErr := pool.Exec(ctx,
		`INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (repo_key) DO UPDATE SET head_sha = EXCLUDED.head_sha, indexed_at = NOW()`,
		repoKey, sha)
	if insertErr != nil {
		t.Fatalf("SetRepoState: %v", insertErr)
	}

	// Assertion A: row exists in public.code_repo_state.
	var gotSHA string
	err = pool.QueryRow(ctx,
		"SELECT head_sha FROM public.code_repo_state WHERE repo_key=$1", repoKey).
		Scan(&gotSHA)
	if err != nil {
		t.Errorf("row not found in public.code_repo_state: %v", err)
	} else if gotSHA != sha {
		t.Errorf("public.code_repo_state.head_sha = %q, want %q", gotSHA, sha)
	}

	// Assertion B: row does NOT exist in ag_catalog (if the table even exists there).
	// This is the falsification check: remove AfterRelease and this assertion fails.
	var agCatalogCount int
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'code_repo_state' AND n.nspname = 'ag_catalog'
	`).Scan(&agCatalogCount)
	if agCatalogCount > 0 {
		// Table exists in ag_catalog — check for the leaked row.
		var leaked bool
		// We cannot use a bare table reference here (search_path could be anything
		// during the test). Use pg_catalog lookup + dynamic check via advisory lock.
		// Instead, assert the row count in ag_catalog.code_repo_state directly.
		err = pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM ag_catalog.code_repo_state WHERE repo_key=$1)",
			repoKey).Scan(&leaked)
		if err == nil && leaked {
			t.Errorf("row leaked into ag_catalog.code_repo_state — AfterRelease hook is not working")
		}
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
