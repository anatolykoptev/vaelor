package pgutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ownershipTransferFailedTotal counts failed ALTER TABLE … OWNER TO CURRENT_USER
// attempts, by table. A nonzero value means the connected role does not own its
// own tables (e.g. after a superuser-run migration or a restore) AND cannot
// re-own them (not a superuser) — the advisory ownership transfer cannot run,
// so metadata updates such as the index staleness marker in code_graph_meta
// silently freeze until ownership is normalized. Alert on any increase so the
// freeze is never silent (issue #520).
//
//	gocode_table_ownership_transfer_failed_total{table="code_graph_meta"} 3
var ownershipTransferFailedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_table_ownership_transfer_failed_total",
		Help: "Failed ALTER TABLE OWNER TO CURRENT_USER attempts by table; nonzero means the app role does not own its tables and metadata updates (e.g. the index staleness marker) will freeze.",
	},
	[]string{"table"},
)

// QueryExecer is the minimal pgx surface TransferOwnership needs to guard the
// ALTER with an ownership pre-check.
// *pgxpool.Conn, *pgxpool.Pool, and pgx.Tx all satisfy this interface.
type QueryExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TransferOwnership runs ALTER TABLE <table> OWNER TO CURRENT_USER as an
// idempotent, privilege-guarded, fail-soft op.
//
// Idempotent + quiet (issue #520): a pg_tables pre-check skips the ALTER
// entirely when the connected role already owns the table, so on an
// already-correct database the sweep is a pure no-op — zero ALTERs, zero
// warnings, zero metric bumps — and repeated EnsureGraph/rebuild calls do not
// spam. When the table is owned by ANOTHER role the pre-check lets the ALTER
// proceed only far enough for Postgres to decide: a superuser re-owns it
// (success, no log); a non-superuser gets SQLSTATE 42501
// (insufficient_privilege), which is logged at DEBUG (not WARN — it recurs on
// every rebuild until an operator heals ownership, so a WARN would spam) and
// bumps gocode_table_ownership_transfer_failed_total so the persistent
// mismatch (which silently freezes the index marker) stays alertable without
// flooding logs. All other errors are logged at WARN and swallowed —
// ownership transfer is advisory; callers must rely on explicit DML grants as
// the hard guarantee.
//
// logPrefix is prepended to log messages (e.g. "codegraph", "embeddings") so
// log lines stay attributable to the calling subsystem.
//
// table may be schema-qualified ("public.code_graph_meta") or bare
// ("code_graph_meta"); a bare name is assumed to live in the public schema.
//
// The SQL keyword CURRENT_USER is a Postgres built-in, not a bind parameter,
// so it resolves to the connected role regardless of the DATABASE_URL role name.
//
// NOTE: The string "CURRENT_USER" must appear in the generated SQL — tests
// assert this to prevent accidental hardcoding of a role name.
func TransferOwnership(ctx context.Context, ex QueryExecer, logPrefix, table string) {
	schema, tbl := splitTable(table)

	// Pre-check: skip the ALTER when the app role already owns the table.
	// COALESCE collapses the no-row case (table does not exist yet) to false,
	// so a missing table is treated as "not owned by us, do not attempt" —
	// ALTERing a nonexistent relation would only log noise.
	var ownedByCurrent bool
	if err := ex.QueryRow(ctx,
		`SELECT COALESCE((SELECT tableowner = current_user
		                    FROM pg_tables
		                   WHERE schemaname = $1 AND tablename = $2), false)`,
		schema, tbl,
	).Scan(&ownedByCurrent); err != nil {
		// A query failure (e.g. transient connection error) is not fatal —
		// ownership transfer is advisory. Log at DEBUG and skip the ALTER.
		slog.Debug(logPrefix+": ownership pre-check query failed; skipping transfer",
			slog.String("table", table), slog.Any("error", err))
		return
	}
	if ownedByCurrent {
		// Already the owner — ALTER OWNER TO CURRENT_USER would be a server-side
		// no-op, but skipping it keeps the sweep quiet (zero ALTERs on a correct
		// DB) and avoids re-issuing a doomed ALTER when another role owns the
		// table and we are not a superuser (the 42501 would recur every rebuild).
		return
	}

	// CURRENT_USER is a SQL keyword — do NOT replace with a bind parameter.
	sql := fmt.Sprintf(`ALTER TABLE %s OWNER TO CURRENT_USER`, table) //nolint:gosec // table is always a package-level constant
	if _, err := ex.Exec(ctx, sql); err != nil {
		if isInsufficientPrivilege(err) {
			// Not the owner and not a superuser — cannot re-own. This recurs on
			// every rebuild until an operator normalizes ownership, so log at
			// DEBUG (a WARN would spam) and bump the alertable counter.
			ownershipTransferFailedTotal.WithLabelValues(table).Inc()
			slog.Debug(logPrefix+": cannot transfer table ownership (not current owner, not superuser); "+
				"ensure explicit DML grants are in place",
				slog.String("table", table))
			return
		}
		slog.Warn(logPrefix+": transfer table owner",
			slog.String("table", table), slog.Any("error", err))
	}
}

// splitTable splits a schema-qualified table name "schema.table" into its
// parts. A bare name (no dot) defaults to the public schema — matching how
// the codegraph metadata tables are created (public.-qualified) and how the
// embeddings/designmd/learnings stores qualify their tables.
func splitTable(name string) (schema, table string) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "public", name
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

// OwnershipTransferFailedTotalForTest exposes the failure counter for
// cross-package integration tests (e.g. internal/codegraph's #520 self-heal
// tests assert it does/does not move across a sweep). Test-only surface; do
// not use in production code.
func OwnershipTransferFailedTotalForTest(table string) prometheus.Counter {
	return ownershipTransferFailedTotal.WithLabelValues(table)
}
