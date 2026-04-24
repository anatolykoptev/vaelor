package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}

	// create_graph raises an error if the graph already exists; suppress it.
	_, err = conn.Exec(ctx, fmt.Sprintf(`SELECT ag_catalog.create_graph('%s')`, name))
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create graph %q: %w", name, err)
	}

	// Ensure the meta table exists in the default schema.
	if _, err := conn.Exec(ctx, metaTableSQL); err != nil {
		return fmt.Errorf("ensure meta table: %w", err)
	}

	// Ensure the file mtimes table exists for incremental indexing.
	if _, err := conn.Exec(ctx, mtimeTableSQL); err != nil {
		return fmt.Errorf("ensure mtimes table: %w", err)
	}

	// Ensure the snapshot table exists for graph diffing.
	if _, err := conn.Exec(ctx, snapshotTableSQL); err != nil {
		return fmt.Errorf("ensure snapshot table: %w", err)
	}

	// Ensure dead code scores table exists for pre-computed reranker scores.
	if _, err := conn.Exec(ctx, deadCodeScoresTableSQL); err != nil {
		return fmt.Errorf("ensure dead_code_scores table: %w", err)
	}

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
		if _, err := conn.Exec(ctx, sql); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create vlabel %q: %w", label, err)
		}
	}

	for _, label := range knownELabels {
		sql := fmt.Sprintf("SELECT ag_catalog.create_elabel('%s', '%s')", gname, label)
		if _, err := conn.Exec(ctx, sql); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create elabel %q: %w", label, err)
		}
	}

	return nil
}
