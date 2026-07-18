package pgutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ownershipTransferFailedTotal counts failed ALTER TABLE … OWNER TO CURRENT_USER
// attempts, by table. A nonzero value means the connected role does not own its
// own tables (e.g. after a superuser-run migration or a restore) — the advisory
// ownership transfer cannot run, so metadata updates such as the index staleness
// marker in code_graph_meta silently freeze until ownership is normalized. Alert
// on any increase so the freeze is never silent (issue #520).
//
//	gocode_table_ownership_transfer_failed_total{table="code_graph_meta"} 3
var ownershipTransferFailedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_table_ownership_transfer_failed_total",
		Help: "Failed ALTER TABLE OWNER TO CURRENT_USER attempts by table; nonzero means the app role does not own its tables and metadata updates (e.g. the index staleness marker) will freeze.",
	},
	[]string{"table"},
)

// Execer is the minimal pgx surface TransferOwnership needs.
// *pgxpool.Conn, *pgxpool.Pool, and pgx.Tx all satisfy this interface.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TransferOwnership runs ALTER TABLE <table> OWNER TO CURRENT_USER as an
// idempotent fail-soft op.
//
// Best-effort: if the connected role is not the current table owner (common
// after a restore where a superuser created the tables), the ALTER fails with
// SQLSTATE 42501 (insufficient_privilege) and we log a warning instead of
// returning an error. All other errors are also logged as warnings and
// swallowed — ownership transfer is advisory; callers must rely on explicit
// DML grants as the hard guarantee. Every failure bumps
// gocode_table_ownership_transfer_failed_total so a persistent ownership
// mismatch (which silently freezes the index marker) is alertable.
//
// logPrefix is prepended to log messages (e.g. "codegraph", "embeddings") so
// log lines stay attributable to the calling subsystem.
//
// The SQL keyword CURRENT_USER is a Postgres built-in, not a bind parameter,
// so it resolves to the connected role regardless of the DATABASE_URL role name.
//
// NOTE: The string "CURRENT_USER" must appear in the generated SQL — tests
// assert this to prevent accidental hardcoding of a role name.
func TransferOwnership(ctx context.Context, ex Execer, logPrefix, table string) {
	// CURRENT_USER is a SQL keyword — do NOT replace with a bind parameter.
	sql := fmt.Sprintf(`ALTER TABLE %s OWNER TO CURRENT_USER`, table) //nolint:gosec // table is always a package-level constant
	if _, err := ex.Exec(ctx, sql); err != nil {
		ownershipTransferFailedTotal.WithLabelValues(table).Inc()
		if isInsufficientPrivilege(err) {
			slog.Warn(logPrefix+": cannot transfer table ownership (not current owner); "+
				"ensure explicit DML grants are in place",
				slog.String("table", table))
			return
		}
		slog.Warn(logPrefix+": transfer table owner",
			slog.String("table", table), slog.Any("error", err))
	}
}

// isInsufficientPrivilege reports whether err is SQLSTATE 42501
// (insufficient_privilege / "must be owner of …").
func isInsufficientPrivilege(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42501"
	}
	return false
}
