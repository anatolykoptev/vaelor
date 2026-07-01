// Package pgtest holds shared test-only helpers for pgvector store
// integration tests (analogous in spirit to stdlib's net/http/httptest:
// a dedicated test-support package, never imported by production code).
package pgtest

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RestoreDSNs reads the two DSNs an ownership-transfer restore-simulation
// test needs and skips the test if either is unset:
//   - DATABASE_URL: the role the store under test connects as (the "app" role).
//   - OWNERSHIP_TEST_ADMIN_DSN: a different, already-privileged role that
//     stands in for the pg_restore-time role and pre-creates the table.
func RestoreDSNs(t *testing.T) (appDSN, adminDSN string) {
	t.Helper()
	appDSN = os.Getenv("DATABASE_URL")
	adminDSN = os.Getenv("OWNERSHIP_TEST_ADMIN_DSN")
	if appDSN == "" || adminDSN == "" {
		t.Skip("DATABASE_URL / OWNERSHIP_TEST_ADMIN_DSN not set; skipping ownership-transfer integration test")
	}
	return appDSN, adminDSN
}

// SimulateRestore connects as adminDSN, refuses to run against anything but
// an obvious throwaway test/CI database (guards against DROPping a real
// table if OWNERSHIP_TEST_ADMIN_DSN is ever pointed at a live DB), then
// (re)creates table via schemaSQL under the admin role — mirroring a
// superuser pg_restore that leaves the table owned by the restoring role.
//
// Returns the admin pool (closed automatically via t.Cleanup) and the
// table's owner immediately after creation, for the caller to compare
// against the owner after exercising the store's init path.
func SimulateRestore(ctx context.Context, t *testing.T, adminDSN, schemaSQL, table string) (adminPool *pgxpool.Pool, ownerBefore string) {
	t.Helper()

	pool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)

	var dbName string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&dbName); err != nil {
		t.Fatalf("query current_database: %v", err)
	}
	lower := strings.ToLower(dbName)
	if !strings.Contains(lower, "test") && !strings.Contains(lower, "ci") {
		t.Skipf("refusing to DROP against a non-test DB %q; OWNERSHIP_TEST_ADMIN_DSN must point at a throwaway database whose name contains \"test\" or \"ci\"", dbName)
	}

	// table is always a package-level constant passed by the calling test,
	// never external input.
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil { //nolint:gosec
		t.Fatalf("drop %s: %v", table, err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("admin create schema: %v", err)
	}
	return pool, TableOwner(ctx, t, pool, table)
}

// TableOwner returns table's current owner per pg_tables.
func TableOwner(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string) string {
	t.Helper()
	var owner string
	if err := pool.QueryRow(ctx, "SELECT tableowner FROM pg_tables WHERE tablename = $1", table).Scan(&owner); err != nil {
		t.Fatalf("query owner of %q: %v", table, err)
	}
	return owner
}
