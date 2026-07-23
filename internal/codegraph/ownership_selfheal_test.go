package codegraph

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/anatolykoptev/vaelor/internal/pgutil"
)

// metadataTables lists the four bookkeeping tables EnsureGraph creates.
// Used by the #520 self-heal tests below.
var metadataTables = []string{
	"code_graph_meta",
	"code_file_mtimes",
	"code_graph_snapshots",
	"code_dead_code_scores",
}

// tableSchemaOwner returns (schemaname, tableowner) for table from pg_tables,
// searching both public and ag_catalog so the test can detect a leak into
// ag_catalog regardless of which schema holds the table.
func tableSchemaOwner(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string) (schema, owner string) {
	t.Helper()
	err := pool.QueryRow(ctx, `
		SELECT schemaname, tableowner
		FROM pg_tables
		WHERE tablename = $1
		  AND schemaname IN ('public', 'ag_catalog')`,
		table).Scan(&schema, &owner)
	if err != nil {
		t.Fatalf("query pg_tables for %q: %v", table, err)
	}
	return schema, owner
}

// dropMetadataTables drops the four metadata tables from BOTH public and
// ag_catalog so the next EnsureGraph creates them fresh — making the
// "created in public" assertion falsifiable (a bare/unqualified DDL would
// land them in ag_catalog via the ageSetup search_path and the assertion
// would fail). Test-only, runs against the ephemeral CI DB.
func dropMetadataTables(ctx context.Context, ex execer) {
	for _, tbl := range metadataTables {
		_, _ = ex.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS public.%s`, tbl))
		_, _ = ex.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS ag_catalog.%s`, tbl))
	}
}

// execer is the minimal Exec surface the test helpers need; satisfied by both
// *pgx.Conn and *pgxpool.Pool.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TestEnsureGraph_CreatesMetadataTablesInPublicOwnedByApp is the load-bearing
// leak-prevention test for issue #520.
//
// On a fresh DB (metadata tables dropped from both public and ag_catalog),
// EnsureGraph must create all four metadata tables in the APP schema (public),
// owned by the connecting role from birth — never in ag_catalog. The leak
// source being fixed: EnsureGraph runs ageSetup (SET search_path TO
// ag_catalog, "$user", public) BEFORE the CREATE TABLE, so an UNqualified
// CREATE TABLE resolves to ag_catalog and the table leaks there. The DDL is
// now public.-qualified, forcing creation into public under the app's own
// connection.
//
// Falsification (red-on-revert): revert any of the four DDLs to an
// unqualified `CREATE TABLE IF NOT EXISTS <name>` and re-run — the table
// lands in ag_catalog (schemaname=ag_catalog) and the schemaname assertion
// fails. Confirmed locally before the fix.
//
// Gated on DATABASE_URL (mirrors meta_agesetup_test etc. — NOT on
// testing.Short, so it runs in CI's live AGE under `make preflight`).
func TestEnsureGraph_CreatesMetadataTablesInPublicOwnedByApp(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	// Identify the connecting role (the expected owner-from-birth).
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	var appRole string
	if err := setup.QueryRow(ctx, `SELECT current_user`).Scan(&appRole); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("identify current_user: %v", err)
	}
	// Fresh state: drop the metadata tables from both schemas.
	dropMetadataTables(ctx, setup)
	_ = setup.Close(ctx)

	const graphName = "code_520_leak_test"
	// Clean up any prior graph of this name so create_graph is the real path.
	cleanupGraph := func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, fmt.Sprintf(`SELECT ag_catalog.drop_graph('%s', true)`, graphName))
		for _, tbl := range metadataTables {
			_, _ = c.Exec(ctx, fmt.Sprintf(`DELETE FROM public.%s WHERE repo_key = $1`, tbl), graphName)
		}
	}
	cleanupGraph()
	t.Cleanup(func() {
		cleanupGraph()
		// Leave the metadata tables in a sane (public) state for other tests.
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, metaTableSQL)
		_, _ = c.Exec(ctx, metaTableMigrateSQL)
		_, _ = c.Exec(ctx, mtimeTableSQL)
		_, _ = c.Exec(ctx, snapshotTableSQL)
		_, _ = c.Exec(ctx, deadCodeScoresTableSQL)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	// Seed a row into code_graph_meta so EnsureGraph's create path is exercised
	// against a genuinely fresh schema (and the table is not just empty-DDL'd).
	if err := store.EnsureGraph(ctx, graphName); err != nil {
		t.Fatalf("EnsureGraph: %v", err)
	}

	for _, tbl := range metadataTables {
		schema, owner := tableSchemaOwner(ctx, t, pool, tbl)
		if schema != "public" {
			t.Errorf("table %q: schemaname = %q, want %q — metadata table leaked into ag_catalog (the #520 bug); DDL must be public.-qualified",
				tbl, schema, "public")
		}
		if owner != appRole {
			t.Errorf("table %q: tableowner = %q, want %q (the connecting role must own it from birth)",
				tbl, owner, appRole)
		}
	}
}

// TestEnsureGraph_OwnershipSweep_IdempotentNoOp verifies the belt-and-suspenders
// ownership sweep is a pure no-op on an already-correct DB: running EnsureGraph
// twice changes no table owner, raises no error, and does NOT bump
// gocode_table_ownership_transfer_failed_total (the 42501 path is never hit
// because the app already owns the tables).
//
// Falsification (red-on-revert): remove the ownership pre-check guard from
// pgutil.TransferOwnership so it ALWAYS issues ALTER TABLE OWNER TO
// CURRENT_USER. Under a superuser that is still a server-side no-op (owner
// unchanged, no 42501), so this test stays GREEN — the no-op-ness under
// superuser is not solely what it guards. The load-bearing assertion here is
// that EnsureGraph succeeds twice AND the tables remain in public owned by
// the app (the leak-prevention contract). The guard's quietness on the
// non-owner path is falsified DB-free by pgutil.TestTransferOwnership's
// "already owner: skip ALTER entirely" case (asserts Exec is NOT called).
//
// Gated on DATABASE_URL — runs in CI's live AGE.
func TestEnsureGraph_OwnershipSweep_IdempotentNoOp(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	var appRole string
	if err := setup.QueryRow(ctx, `SELECT current_user`).Scan(&appRole); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("identify current_user: %v", err)
	}
	// Fresh state so the first EnsureGraph is the creation path.
	dropMetadataTables(ctx, setup)
	_ = setup.Close(ctx)

	const graphName = "code_520_idempotent_test"
	cleanupGraph := func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, fmt.Sprintf(`SELECT ag_catalog.drop_graph('%s', true)`, graphName))
		for _, tbl := range metadataTables {
			_, _ = c.Exec(ctx, fmt.Sprintf(`DELETE FROM public.%s WHERE repo_key = $1`, tbl), graphName)
		}
	}
	cleanupGraph()
	t.Cleanup(func() {
		cleanupGraph()
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, metaTableSQL)
		_, _ = c.Exec(ctx, metaTableMigrateSQL)
		_, _ = c.Exec(ctx, mtimeTableSQL)
		_, _ = c.Exec(ctx, snapshotTableSQL)
		_, _ = c.Exec(ctx, deadCodeScoresTableSQL)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	// First run: creates tables + sweep (app owns from birth → guard skips).
	if err := store.EnsureGraph(ctx, graphName); err != nil {
		t.Fatalf("first EnsureGraph: %v", err)
	}
	ownersBefore := make(map[string]string, len(metadataTables))
	for _, tbl := range metadataTables {
		_, ownersBefore[tbl] = tableSchemaOwner(ctx, t, pool, tbl)
	}
	metricBefore := testutil.ToFloat64(pgutil.OwnershipTransferFailedTotalForTest("public.code_graph_meta"))

	// Second run: must be a no-op for the sweep — no error, no owner change,
	// no failure-metric bump.
	if err := store.EnsureGraph(ctx, graphName); err != nil {
		t.Fatalf("second EnsureGraph: %v", err)
	}
	for _, tbl := range metadataTables {
		schema, ownerAfter := tableSchemaOwner(ctx, t, pool, tbl)
		if schema != "public" {
			t.Errorf("table %q: schemaname drifted to %q after second EnsureGraph", tbl, schema)
		}
		if ownerAfter != ownersBefore[tbl] {
			t.Errorf("table %q: owner changed across no-op sweep: %q -> %q", tbl, ownersBefore[tbl], ownerAfter)
		}
		if ownerAfter != appRole {
			t.Errorf("table %q: owner = %q, want app role %q", tbl, ownerAfter, appRole)
		}
	}
	metricAfter := testutil.ToFloat64(pgutil.OwnershipTransferFailedTotalForTest("public.code_graph_meta"))
	if delta := metricAfter - metricBefore; delta != 0 {
		t.Errorf("gocode_table_ownership_transfer_failed_total{code_graph_meta}: want +0 across no-op sweep, got +%v", delta)
	}
}

// TestOwnershipSweep_GuardSkipsGracefullyWhenNotOwner verifies the
// privilege-guarded sweep does not crash — and does not spam a WARN — when the
// metadata table is owned by ANOTHER role and the connecting role is a
// non-superuser (the exact prod condition that froze the index marker).
//
// Setup: as the superuser (DATABASE_URL), create a non-superuser LOGIN role
// and a public.code_graph_meta owned by the superuser. Then connect as the
// non-superuser and call pgutil.TransferOwnership directly: the pre-check
// sees owner != current_user → attempts ALTER → Postgres returns 42501 → the
// guard logs at DEBUG, bumps the failure counter, and returns (no panic). The
// table owner must remain the superuser (graceful skip, no change).
//
// This runs in CI (DATABASE_URL only): the non-superuser role is created
// inside the test against the ephemeral CI DB. The 42501-graceful-skip LOGIC
// is additionally falsified DB-free by pgutil.TestTransferOwnership's
// "not owner, 42501" case (asserts no panic + metric +1).
//
// Skipped when the connecting role is not a superuser (cannot CREATE ROLE) or
// when role-authenticated TCP connect is unavailable.
func TestOwnershipSweep_GuardSkipsGracefullyWhenNotOwner(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	// Only a superuser can CREATE ROLE; skip gracefully on a non-superuser DSN.
	var isSuper bool
	if err := admin.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&isSuper); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("probe rolsuper: %v", err)
	}
	_ = admin.Close(ctx)
	if !isSuper {
		t.Skip("DATABASE_URL role is not superuser — cannot provision a non-superuser app role for this test")
	}

	const (
		appRoleName = "gocode_520_test_nosuper"
		appRolePwd  = "test"
	)
	adminPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer adminPool.Close()

	// Provision a non-superuser LOGIN role with USAGE on public (so the
	// pg_tables pre-check can see the table) and clean up at the end.
	if _, err := adminPool.Exec(ctx,
		fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER`, appRoleName, appRolePwd),
	); err != nil {
		t.Skipf("create role %q: %v (auth setup unsupported in this environment)", appRoleName, err)
	}
	t.Cleanup(func() {
		// Re-own the table back to the admin role so DROP ROLE succeeds
		// (cannot drop a role that still owns objects).
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(`ALTER TABLE public.code_graph_meta OWNER TO current_user`))
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %s`, appRoleName))
	})
	if _, err := adminPool.Exec(ctx,
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, appRoleName),
	); err != nil {
		t.Fatalf("grant usage: %v", err)
	}

	// Ensure public.code_graph_meta exists and is owned by the admin (superuser)
	// — the "owned by another role" precondition.
	if _, err := adminPool.Exec(ctx, metaTableSQL); err != nil {
		t.Fatalf("ensure meta table: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `ALTER TABLE public.code_graph_meta OWNER TO current_user`); err != nil {
		t.Fatalf("set meta table owner to admin: %v", err)
	}
	adminOwnerBefore, err := tableOwnerOf(ctx, adminPool, "public", "code_graph_meta")
	if err != nil {
		t.Fatalf("read owner before: %v", err)
	}

	// Connect as the non-superuser app role.
	appDSN := withRole(dbURL, appRoleName, appRolePwd)
	appPool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatalf("app pool parse: %v", err)
	}
	defer appPool.Close()
	// Probe the connection actually authenticates as the non-superuser.
	var connectedRole string
	if err := appPool.QueryRow(ctx, `SELECT current_user`).Scan(&connectedRole); err != nil {
		t.Skipf("cannot connect as non-superuser %q (auth): %v", appRoleName, err)
	}
	if connectedRole != appRoleName {
		t.Fatalf("connected as %q, want %q", connectedRole, appRoleName)
	}

	metricBefore := testutil.ToFloat64(pgutil.OwnershipTransferFailedTotalForTest("public.code_graph_meta"))

	// The decisive call: must not panic, must skip gracefully.
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				t.Errorf("TransferOwnership panicked on 42501 (must be fail-soft): %v", r)
			}
		}()
		pgutil.TransferOwnership(ctx, appPool, "codegraph", "public.code_graph_meta")
	}()
	if didPanic {
		t.FailNow()
	}

	// Owner unchanged: the graceful skip must NOT have re-owned the table.
	ownerAfter, err := tableOwnerOf(ctx, adminPool, "public", "code_graph_meta")
	if err != nil {
		t.Fatalf("read owner after: %v", err)
	}
	if ownerAfter != adminOwnerBefore {
		t.Errorf("table owner changed across guarded skip: %q -> %q (a non-superuser must not be able to re-own)",
			adminOwnerBefore, ownerAfter)
	}

	// The failure counter must bump exactly once (the alertable signal that
	// ownership is wrong) — but no WARN log is emitted (covered DB-free).
	metricAfter := testutil.ToFloat64(pgutil.OwnershipTransferFailedTotalForTest("public.code_graph_meta"))
	if delta := metricAfter - metricBefore; delta != 1 {
		t.Errorf("gocode_table_ownership_transfer_failed_total{code_graph_meta}: want +1 on graceful 42501 skip, got +%v", delta)
	}
}

// tableOwnerOf returns the owner of schema.table via pg_tables.
func tableOwnerOf(ctx context.Context, pool *pgxpool.Pool, schema, table string) (string, error) {
	var owner string
	err := pool.QueryRow(ctx,
		`SELECT tableowner FROM pg_tables WHERE schemaname = $1 AND tablename = $2`,
		schema, table).Scan(&owner)
	return owner, err
}

// withRole returns a DSN equivalent to dsn but authenticating as user/pwd.
// It handles the common `postgres://user:pass@host:port/db?...` form by
// splicing the user/password segment, and falls back to setting the user via
// the `user`/`password` query params for key=value DSNs.
func withRole(dsn, user, pwd string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// Split into scheme://userinfo@hostpart.
		schemeEnd := strings.Index(dsn, "://") + 3
		rest := dsn[schemeEnd:]
		atIdx := strings.Index(rest, "@")
		if atIdx >= 0 {
			hostpart := rest[atIdx+1:]
			return dsn[:schemeEnd] + user + ":" + pwd + "@" + hostpart
		}
		// No userinfo: insert it.
		return dsn[:schemeEnd] + user + ":" + pwd + "@" + rest
	}
	// key=value DSN: append/override user and password.
	if strings.Contains(dsn, "user=") {
		dsn = strings.Split(dsn, "user=")[0] + "user=" + user + " password=" + pwd + " " + strings.SplitN(dsn, "user=", 2)[1]
		return dsn
	}
	return dsn + " user=" + user + " password=" + pwd
}
