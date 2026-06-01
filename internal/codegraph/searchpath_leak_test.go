package codegraph

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	dto "github.com/prometheus/client_model/go"
)

// testPoolMaxConns1 builds a pgxpool with MaxConns=1 and the AfterRelease hook that
// SR-A wires in register.go. MaxConns=1 forces dirty-conn reuse: after an
// acquireAGE call the single connection is returned to the pool and immediately
// re-acquired by the next caller — if the hook does not clean the conn, the
// data-path statement runs inside the ag_catalog search_path set by acquireAGE.
//
// The hook uses RESET ALL (matches register.go) — this resets search_path and
// every other session GUC (synchronous_commit, statement_timeout) to the role
// default, WITHOUT deallocating prepared statements (DISCARD ALL would, which
// breaks pgx's statement cache — see TestPreparedStmtSurvivesRelease_MaxConns1).
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
	// RESET ALL resets search_path + all session-level GUCs (synchronous_commit,
	// statement_timeout, etc.) to their role default. It deliberately does NOT
	// run DEALLOCATE ALL: pgx default exec mode caches prepared statements
	// per-conn and DISCARD ALL would invalidate them server-side, yielding 26000.
	cfg.AfterRelease = func(conn *pgx.Conn) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, "RESET ALL"); err != nil {
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
// search_path via acquireAGE, releases it (triggering AfterRelease → RESET ALL),
// then issues a BARE (unqualified) INSERT INTO code_repo_state. With the hook the
// bare name resolves to public.code_repo_state; without it the name resolves to
// ag_catalog.code_repo_state (the AGE-dirty search_path wins).
//
// Falsification (red-on-revert): stash the AfterRelease body (replace with
// `return true` — no RESET ALL), run the test, confirm it fails at assertion B
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
		// public.code_repo_state is the REAL table — only delete our fixture row.
		_, _ = pool.Exec(bg, "DELETE FROM public.code_repo_state WHERE repo_key=$1", repoKey)
		// ag_catalog.code_repo_state must NOT exist in steady state — it is purely a
		// leak orphan / this test's fixture. DROP it (not just DELETE the row): a
		// leftover empty table trips AssertSchemaDrift (gocode_schema_drift_total) on
		// the next boot when these tests run against the live gocode DB. Matches the
		// cleanup in TestSchemaDriftAssertion_CounterBumped.
		_, _ = pool.Exec(bg, "DROP TABLE IF EXISTS ag_catalog.code_repo_state")
	})

	// Step 1: dirty the single connection with AGE search_path.
	// acquireAGE runs SET search_path TO ag_catalog, "$user", public.
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Skipf("AGE not available: %v", err)
	}
	// Release triggers AfterRelease → RESET ALL → search_path reset to default.
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
		t.Errorf("row leaked into ag_catalog.code_repo_state — AfterRelease RESET ALL hook is not working")
	}
}

// TestPreparedStmtSurvivesRelease_MaxConns1 is the falsification test for the
// DISCARD-ALL regression that broke SetRepoState in production (the
// `prepared statement "stmtcache_…" does not exist (SQLSTATE 26000)` storm).
//
// Root cause: pgx's DEFAULT exec mode (QueryExecModeCacheStatement) keeps a
// per-connection server-side prepared-statement cache. DISCARD ALL includes
// DEALLOCATE ALL, which drops those statements server-side while pgx's
// client-side LRU still references them — the next reuse across a Release
// boundary fails with 26000. RESET ALL resets GUCs only and leaves the
// statements intact, so the cache stays consistent.
//
// Setup: MaxConns=1 forces the single physical connection to be reused. We run
// the SAME parameterised query twice through pool.QueryRow; the first call
// prepares+caches the statement, the Release between them fires AfterRelease,
// and the second call reuses the cached statement on the same conn.
//
// Falsification (red-on-revert): change the hook in testPoolMaxConns1 back to
// `DISCARD ALL` and the second QueryRow fails with SQLSTATE 26000 → test FAILS.
// With RESET ALL it succeeds → GREEN.
func TestPreparedStmtSurvivesRelease_MaxConns1(t *testing.T) {
	pool := testPoolMaxConns1(t)
	ctx := context.Background()

	// Parameterised query → pgx uses the extended protocol and caches the
	// prepared statement on the connection (stmtcache_<hash>).
	const q = "SELECT $1::int AS n"

	// Call 1: prepares + caches the statement, then Release fires the hook.
	var n1 int
	if err := pool.QueryRow(ctx, q, 1).Scan(&n1); err != nil {
		t.Fatalf("first QueryRow (prime statement cache): %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first QueryRow returned %d, want 1", n1)
	}

	// Call 2: reuses the cached statement on the same (MaxConns=1) connection
	// across the Release boundary. Under DISCARD ALL this is where 26000 fires.
	var n2 int
	err := pool.QueryRow(ctx, q, 2).Scan(&n2)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "26000" {
			t.Fatalf("prepared statement deallocated across Release (SQLSTATE 26000): %v — "+
				"AfterRelease must NOT run DISCARD ALL / DEALLOCATE ALL with pgx statement caching", err)
		}
		t.Fatalf("second QueryRow (reuse cached statement): %v", err)
	}
	if n2 != 2 {
		t.Fatalf("second QueryRow returned %d, want 2", n2)
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
	// t.Cleanup, NOT defer: the DROP cleanup below is registered later, so LIFO
	// runs DROP first (pool still open) then Close. With `defer pool.Close()` the
	// pool would close before t.Cleanup ran → the DROP would hit a closed pool and
	// silently no-op, leaking ag_catalog.code_repo_state into the live gocode DB.
	t.Cleanup(pool.Close)
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
