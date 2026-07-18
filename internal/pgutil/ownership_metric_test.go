package pgutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// execFunc adapts a plain func to the Execer interface for tests.
type execFunc func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)

func (f execFunc) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f(ctx, sql, args...)
}

// TransferOwnership must bump gocode_table_ownership_transfer_failed_total when
// the ALTER fails with insufficient_privilege (SQLSTATE 42501). That counter is
// the alertable signal that catches a silently-frozen index marker (issue #520):
// before this, the failure was only a WARN and the freeze went unnoticed for weeks.
func TestTransferOwnership_BumpsMetricOnInsufficientPrivilege(t *testing.T) {
	const table = "test_ownership_metric_tbl"
	before := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))

	ex := execFunc(func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "42501"}
	})
	TransferOwnership(context.Background(), ex, "test", table)

	if got := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table)) - before; got != 1 {
		t.Errorf("gocode_table_ownership_transfer_failed_total{table=%q}: want +1, got +%v", table, got)
	}
}

// A successful transfer must NOT bump the failure counter.
func TestTransferOwnership_NoMetricOnSuccess(t *testing.T) {
	const table = "test_ownership_ok_tbl"
	before := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))

	ex := execFunc(func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("ALTER TABLE"), nil
	})
	TransferOwnership(context.Background(), ex, "test", table)

	if got := testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table)) - before; got != 0 {
		t.Errorf("failure counter moved on success: got +%v", got)
	}
}
