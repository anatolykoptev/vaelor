package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anatolykoptev/go-code/internal/pgutil"
)

// EnsureGraph creates the AGE graph if it does not exist and ensures the
// code_graph_meta bookkeeping table is present.
func (s *Store) EnsureGraph(ctx context.Context, name string) error {
	if err := validateGraphName(name); err != nil {
		return err
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Warn("codegraph: ensure graph rollback", slog.Any("error", err))
		}
	}()

	if _, err := tx.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}

	// Serialize the provisioning sequence across sessions. PostgreSQL's
	// CREATE TABLE IF NOT EXISTS is not concurrency-safe: two sessions can both
	// see a missing table and collide on the pg_type unique index
	// (pg_type_typname_nsp_index, SQLSTATE 23505).
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('codegraph.ensure_graph'))"); err != nil {
		return fmt.Errorf("acquire ensure_graph lock: %w", err)
	}

	// create_graph raises SQLSTATE 42710 (duplicate_object) if the graph already
	// exists. Inside a transaction that error aborts the transaction, so check
	// existence first and only create when the graph is missing.
	var graphExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)", name).Scan(&graphExists); err != nil {
		return fmt.Errorf("check graph existence: %w", err)
	}
	if !graphExists {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`SELECT ag_catalog.create_graph('%s')`, name)); err != nil {
			return fmt.Errorf("create graph %q: %w", name, err)
		}
	}

	// Ensure the meta table exists in the default schema.
	if _, err := tx.Exec(ctx, metaTableSQL); err != nil {
		return fmt.Errorf("ensure meta table: %w", err)
	}

	// Ensure the file mtimes table exists for incremental indexing.
	if _, err := tx.Exec(ctx, mtimeTableSQL); err != nil {
		return fmt.Errorf("ensure mtimes table: %w", err)
	}

	// Ensure the snapshot table exists for graph diffing.
	if _, err := tx.Exec(ctx, snapshotTableSQL); err != nil {
		return fmt.Errorf("ensure snapshot table: %w", err)
	}

	// Ensure dead code scores table exists for pre-computed reranker scores.
	if _, err := tx.Exec(ctx, deadCodeScoresTableSQL); err != nil {
		return fmt.Errorf("ensure dead_code_scores table: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ensure graph: %w", err)
	}

	// Best-effort ownership transfer, run AFTER commit on the autocommit
	// conn (NOT inside the tx above): each ALTER TABLE ... OWNER TO is its
	// own independent statement, deliberately fail-soft (see
	// pgutil.TransferOwnership) for the "restore where a superuser created
	// the tables" / non-owner case (SQLSTATE 42501). Swallowing that error
	// INSIDE a transaction still poisons it server-side (any error aborts
	// the tx; the next statement fails with 25P02 "current transaction is
	// aborted"), which would turn this best-effort step into a hard failure
	// of the whole graph build on any host where the connected role doesn't
	// own a pre-existing bookkeeping table. Running post-commit on conn
	// keeps each transfer independent and preserves the original
	// best-effort semantics. Ownership transfer is idempotent and does not
	// need the advisory-lock serialization above.
	pgutil.TransferOwnership(ctx, conn, "codegraph", "code_graph_meta")
	pgutil.TransferOwnership(ctx, conn, "codegraph", "code_file_mtimes")
	pgutil.TransferOwnership(ctx, conn, "codegraph", "code_graph_snapshots")
	pgutil.TransferOwnership(ctx, conn, "codegraph", "code_dead_code_scores")

	// Seed the exists-cache so subsequent read-path preflight calls don't hit
	// ag_catalog.ag_graph immediately after a build.
	s.existsCache.Mark(name)

	return nil
}

// DropGraph drops the AGE graph and removes the meta row for the given repoKey.
func (s *Store) DropGraph(ctx context.Context, name, repoKey string) error {
	if err := validateGraphName(name); err != nil {
		return err
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}

	_, err = conn.Exec(ctx, fmt.Sprintf(`SELECT ag_catalog.drop_graph('%s', true)`, name))
	if err != nil {
		return fmt.Errorf("drop graph %q: %w", name, err)
	}

	_, err = conn.Exec(ctx, `DELETE FROM code_graph_meta WHERE repo_key = $1`, repoKey)
	if err != nil {
		return fmt.Errorf("delete meta row: %w", err)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM code_file_mtimes WHERE repo_key = $1`, repoKey); err != nil {
		slog.Warn("codegraph: delete mtimes on drop", slog.String("repo", repoKey), slog.Any("error", err))
	}

	return nil
}

// EnsureIndexes creates Postgres indexes on AGE vertex tables to speed up
// property-filter queries (e.g. WHERE s.kind = 'interface').
//
// AGE stores vertices as rows in per-label tables with an `agtype` properties
// column. Filters like `s.kind = 'interface'` translate to
// `properties->>'kind' = 'interface'` which requires a sequential scan without
// an index. Adding expression indexes speeds these queries up 5-10x on large
// graphs (>2000 symbols).
//
// Indexes are created with IF NOT EXISTS and are safe to run on every graph
// rebuild. Non-fatal: index failures log a warning and continue.
func (s *Store) EnsureIndexes(ctx context.Context, name string) error {
	if err := validateGraphName(name); err != nil {
		return err
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// GIN index on agtype `properties` column — indexes all property keys at
	// once. Works for WHERE clauses like `s.kind = 'interface'` because AGE
	// translates those to agtype operators that GIN can accelerate.
	//
	// AGE property-level btree indexes don't work reliably because agtype's
	// `->` operator returns agtype (not jsonb), and casting to text fails
	// with SQLSTATE 22P02.
	labels := []string{"Symbol", "Package", "File"}
	slog.Info("codegraph: ensuring indexes", slog.String("graph", name))
	for _, label := range labels {
		idxName := fmt.Sprintf("idx_%s_%s_props", name, strings.ToLower(label))
		sql := fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %q ON %q.%q USING gin (properties)`,
			idxName, name, label,
		)
		slog.Info("codegraph: creating index", slog.String("sql", sql))
		if _, err := conn.Exec(ctx, sql); err != nil {
			// Non-fatal: missing vertex table means that label has no data yet.
			slog.Warn("codegraph: create index",
				slog.String("graph", name),
				slog.String("label", label),
				slog.Any("error", err))
		}
	}

	return nil
}

// knownVLabels and knownELabels enumerate every label the graph model uses.
// They are pre-created by EnsureLabels before parallel inserts to prevent
// the AGE race condition where concurrent workers all try to create the same
// label table and all but the first fail with "duplicate key".
var (
	knownVLabels = []string{"File", "Layer", "Package", "Route", "Symbol"}
	knownELabels = []string{
		"BELONGS_TO", "CALLS", "CONTAINS", "FETCHES",
		"HANDLES", "IMPLEMENTS", "IMPORTS", "INHERITS",
		"TESTED_BY", "USES",
	}
)

// EnsureLabels pre-creates all known vertex and edge labels for gname.
// Safe to call when labels already exist; "already exists" errors are ignored.
func (s *Store) EnsureLabels(ctx context.Context, gname string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}

	for _, label := range knownVLabels {
		sql := fmt.Sprintf("SELECT ag_catalog.create_vlabel('%s', '%s')", gname, label)
		if _, err := conn.Exec(ctx, sql); err != nil && !isDuplicateObjectError(err) {
			return fmt.Errorf("create vlabel %q: %w", label, err)
		}
	}

	for _, label := range knownELabels {
		sql := fmt.Sprintf("SELECT ag_catalog.create_elabel('%s', '%s')", gname, label)
		if _, err := conn.Exec(ctx, sql); err != nil && !isDuplicateObjectError(err) {
			return fmt.Errorf("create elabel %q: %w", label, err)
		}
	}

	return nil
}

// isDuplicateObjectError reports whether err is SQLSTATE 42710 (duplicate_object),
// which AGE raises when create_graph, create_vlabel, or create_elabel is called
// for an object that already exists. This replaces the fragile strings.Contains
// "already exists" check.
func isDuplicateObjectError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42710" {
		return true
	}
	// Fallback: some AGE versions embed the code only in the message.
	return strings.Contains(err.Error(), "already exists")
}
