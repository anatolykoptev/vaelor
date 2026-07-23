package pgutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// queryExecFunc adapts two plain funcs to the QueryExecer interface for tests.
// The pre-check func returns whether the table is already owned by current_user;
// the exec func returns the ALTER result.
type queryExecFunc struct {
	ownedByCurrent bool
	exec           func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (f *queryExecFunc) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.exec(ctx, sql, args...)
}

func (f *queryExecFunc) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeRow{owned: f.ownedByCurrent}
}

// TransferOwnership must bump gocode_table_ownership_transfer_failed_total when
// the ALTER fails with insufficient_privilege (SQLSTATE 42501) AND the table is
// not already owned by current_user. That counter is the alertable signal that
// catches a silently-frozen index marker (issue #520): before this, the failure
// was only a WARN and the freeze went unnoticed for weeks.
func TestTransferOwnership_BumpsMetricOnInsufficientPrivilege(t *testing.T) {
	const table = "test_ownership_metric_tbl"
	before := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))

	ex := &queryExecFunc{
		ownedByCurrent: false, // not owner → ALTER is attempted
		exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, &pgconn.PgError{Code: "42501"}
		},
	}
	TransferOwnership(context.Background(), ex, "test", table)

	if got := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table)) - before; got != 1 {
		t.Errorf("gocode_table_ownership_transfer_failed_total{table=%q}: want +1, got +%v", table, got)
	}
}

// A successful transfer must NOT bump the failure counter.
func TestTransferOwnership_NoMetricOnSuccess(t *testing.T) {
	const table = "test_ownership_ok_tbl"
	before := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))

	ex := &queryExecFunc{
		ownedByCurrent: false, // not owner → ALTER is attempted and succeeds
		exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("ALTER TABLE"), nil
		},
	}
	TransferOwnership(context.Background(), ex, "test", table)

	if got := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table)) - before; got != 0 {
		t.Errorf("failure counter moved on success: got +%v", got)
	}
}

// When the app already owns the table the sweep is a pure no-op: the ALTER is
// never issued, so the failure counter cannot move even if a later (hypothetical
// re-invocation) exec would error. This is the idempotency guarantee on an
// already-correct DB (issue #520).
func TestTransferOwnership_NoMetricWhenAlreadyOwner(t *testing.T) {
	const table = "test_ownership_already_owner_tbl"
	before := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))

	ex := &queryExecFunc{
		ownedByCurrent: true, // already owner → ALTER skipped entirely
		exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			t.Errorf("Exec (ALTER) must not be called when already owner")
			return pgconn.CommandTag{}, &pgconn.PgError{Code: "42501"}
		},
	}
	TransferOwnership(context.Background(), ex, "test", table)

	if got := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table)) - before; got != 0 {
		t.Errorf("failure counter moved on already-owner no-op: got +%v", got)
	}
}
