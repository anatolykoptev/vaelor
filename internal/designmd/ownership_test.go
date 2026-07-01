package designmd

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEnsureSchema_TransfersOwnershipAfterRestore reproduces the prod
// failure class this guards against: a superuser pg_restore leaves
// design_embeddings owned by the restoring role, so the service role's
// later INSERT/TRUNCATE would fail with "permission denied" unless
// EnsureSchema() reclaims ownership on init.
//
// Requires TWO DSNs since a single-role test can't distinguish "EnsureSchema
// ran the transfer" from "the connecting role already owned the table":
//   - DATABASE_URL: the role Store connects as (the "app" role).
//   - OWNERSHIP_TEST_ADMIN_DSN: a different, already-privileged role that
//     stands in for the pg_restore-time role and pre-creates the table.
//
// Both must be superusers (or otherwise privileged) in this test — Postgres
// only allows ALTER TABLE ... OWNER TO to succeed when the connecting role
// already owns the object or is a superuser, so this test asserts the
// success path pgutil.TransferOwnership is documented to handle. The
// insufficient-privilege fallback (log-and-continue) is covered separately
// by pgutil.TestTransferOwnership's mock-Execer unit test.
func TestEnsureSchema_TransfersOwnershipAfterRestore(t *testing.T) {
	appDSN := os.Getenv("DATABASE_URL")
	adminDSN := os.Getenv("OWNERSHIP_TEST_ADMIN_DSN")
	if appDSN == "" || adminDSN == "" {
		t.Skip("DATABASE_URL / OWNERSHIP_TEST_ADMIN_DSN not set; skipping ownership-transfer integration test")
	}
	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin pgxpool: %v", err)
	}
	defer adminPool.Close()

	// Simulate a superuser pg_restore: (re)create the table under the admin
	// role BEFORE the app role ever connects, so it starts out owned by admin.
	if _, err := adminPool.Exec(ctx, "DROP TABLE IF EXISTS design_embeddings"); err != nil {
		t.Fatalf("drop design_embeddings: %v", err)
	}
	if _, err := adminPool.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("admin create schema: %v", err)
	}
	ownerBefore := tableOwner(ctx, t, adminPool, "design_embeddings")

	// EnsureSchema connects as the (differently-privileged) app role. The
	// table already exists, so CREATE TABLE IF NOT EXISTS is a no-op on
	// schema — only pgutil.TransferOwnership (called from EnsureSchema) can
	// change the owner.
	appPool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatalf("app pgxpool: %v", err)
	}
	defer appPool.Close()

	s := NewStore(appPool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	ownerAfter := tableOwner(ctx, t, adminPool, "design_embeddings")
	if ownerAfter == ownerBefore {
		t.Fatalf("table owner unchanged after EnsureSchema() (still %q); TransferOwnership was not applied on init", ownerAfter)
	}
}

func tableOwner(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string) string {
	t.Helper()
	var owner string
	if err := pool.QueryRow(ctx, "SELECT tableowner FROM pg_tables WHERE tablename = $1", table).Scan(&owner); err != nil {
		t.Fatalf("query owner of %q: %v", table, err)
	}
	return owner
}
