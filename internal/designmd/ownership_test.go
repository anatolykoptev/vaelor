package designmd

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/vaelor/internal/pgutil/pgtest"
)

// TestEnsureSchema_TransfersOwnershipAfterRestore reproduces the prod
// failure class this guards against: a superuser pg_restore leaves
// design_embeddings owned by the restoring role, so the service role's
// later INSERT/TRUNCATE would fail with "permission denied" unless
// EnsureSchema() reclaims ownership on init.
//
// Both DSNs must be superusers (or otherwise privileged) — Postgres only
// allows ALTER TABLE ... OWNER TO to succeed when the connecting role
// already owns the object or is a superuser, so this test asserts the
// success path pgutil.TransferOwnership is documented to handle. The
// insufficient-privilege fallback (log-and-continue) is covered separately
// by pgutil.TestTransferOwnership's mock-Execer unit test.
func TestEnsureSchema_TransfersOwnershipAfterRestore(t *testing.T) {
	appDSN, adminDSN := pgtest.RestoreDSNs(t)
	ctx := context.Background()

	// Simulate a superuser pg_restore: (re)create the table under the admin
	// role BEFORE the app role ever connects, so it starts out owned by admin.
	adminPool, ownerBefore := pgtest.SimulateRestore(ctx, t, adminDSN, schemaSQL, "design_embeddings")

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

	ownerAfter := pgtest.TableOwner(ctx, t, adminPool, "design_embeddings")
	if ownerAfter == ownerBefore {
		t.Fatalf("table owner unchanged after EnsureSchema() (still %q); TransferOwnership was not applied on init", ownerAfter)
	}
}
