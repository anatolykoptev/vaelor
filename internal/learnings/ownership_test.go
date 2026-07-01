package learnings

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/pgutil/pgtest"
)

// TestNew_TransfersOwnershipAfterRestore reproduces the prod failure class
// this guards against: a superuser pg_restore leaves review_learnings owned
// by the restoring role, so the service role's later INSERT/TRUNCATE would
// fail with "permission denied" unless New() reclaims ownership on init.
//
// Both DSNs must be superusers (or otherwise privileged) — Postgres only
// allows ALTER TABLE ... OWNER TO to succeed when the connecting role
// already owns the object or is a superuser, so this test asserts the
// success path pgutil.TransferOwnership is documented to handle. The
// insufficient-privilege fallback (log-and-continue) is covered separately
// by pgutil.TestTransferOwnership's mock-Execer unit test.
func TestNew_TransfersOwnershipAfterRestore(t *testing.T) {
	appDSN, adminDSN := pgtest.RestoreDSNs(t)
	ctx := context.Background()

	// Simulate a superuser pg_restore: (re)create the table under the admin
	// role BEFORE the app role ever connects, so it starts out owned by admin.
	adminPool, ownerBefore := pgtest.SimulateRestore(ctx, t, adminDSN, schemaSQL, "review_learnings")

	// New() connects as the (differently-privileged) app role. The table
	// already exists, so CREATE TABLE IF NOT EXISTS is a no-op on schema —
	// only pgutil.TransferOwnership (called from New()) can change the owner.
	s, err := New(ctx, appDSN, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)

	ownerAfter := pgtest.TableOwner(ctx, t, adminPool, "review_learnings")
	if ownerAfter == ownerBefore {
		t.Fatalf("table owner unchanged after New() (still %q); TransferOwnership was not applied on init", ownerAfter)
	}
}
