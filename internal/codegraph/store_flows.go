package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
)

// flowsTableSQL defines the code_flows table schema (ADR-001).
//
// code_flows is a plain relational table, NOT an AGE vertex. It is a
// read-mostly derived projection keyed by repo_key, rebuilt on every
// re-index (delete-then-insert). The name_embedding column is nullable;
// populated later when EMBED_URL is set (FLOWS_IN_SEARCH extension point).
const flowsTableSQL = `
CREATE TABLE IF NOT EXISTS public.code_flows (
    repo_key       TEXT NOT NULL,
    flow_id        TEXT NOT NULL,
    name           TEXT NOT NULL,
    entry_sym      TEXT NOT NULL,
    entry_file     TEXT NOT NULL,
    leaf_sym       TEXT NOT NULL,
    member_syms    TEXT[] NOT NULL DEFAULT '{}',
    priority       DOUBLE PRECISION NOT NULL DEFAULT 0,
    community      TEXT NOT NULL DEFAULT '0',
    name_embedding vector(768),
    PRIMARY KEY (repo_key, flow_id)
);
CREATE INDEX IF NOT EXISTS idx_code_flows_repo ON public.code_flows (repo_key);
CREATE INDEX IF NOT EXISTS idx_code_flows_priority ON public.code_flows (repo_key, priority DESC);`

// EnsureFlowsTable creates the code_flows table if it does not exist.
// Safe to call on every IndexRepo pass (idempotent CREATE IF NOT EXISTS).
// Uses a plain dataPool connection — code_flows lives in public, not ag_catalog.
func (s *Store) EnsureFlowsTable(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("flows: acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, flowsTableSQL); err != nil {
		return fmt.Errorf("flows: ensure table: %w", err)
	}
	return nil
}

// UpsertFlows replaces all flows for repoKey with the provided slice.
// Uses DELETE + batch-INSERT within a single transaction (reconcile pattern).
// Non-fatal: logs on error and bumps flowsDBErrorTotal so alerts fire.
func (s *Store) UpsertFlows(ctx context.Context, repoKey string, flows []Flow) error {
	if err := s.EnsureFlowsTable(ctx); err != nil {
		return fmt.Errorf("flows: schema: %w", err)
	}
	if len(flows) == 0 {
		// Nothing to insert — still delete stale rows from previous runs.
		return s.deleteFlows(ctx, repoKey)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("flows: acquire: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("flows: begin tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err.Error() != "tx is closed" {
			slog.Warn("flows: rollback", slog.Any("error", err))
		}
	}()

	// Delete all existing flows for this repo_key.
	if _, err := tx.Exec(ctx,
		`DELETE FROM public.code_flows WHERE repo_key = $1`, repoKey,
	); err != nil {
		return fmt.Errorf("flows: delete: %w", err)
	}

	// Batch-insert new flows.
	if err := insertFlowsBatch(ctx, tx, repoKey, flows); err != nil {
		return fmt.Errorf("flows: insert batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("flows: commit: %w", err)
	}
	return nil
}

// deleteFlows removes all flows for repoKey (used when flows list is empty).
func (s *Store) deleteFlows(ctx context.Context, repoKey string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("flows: acquire for delete: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx,
		`DELETE FROM public.code_flows WHERE repo_key = $1`, repoKey,
	); err != nil {
		return fmt.Errorf("flows: delete empty: %w", err)
	}
	return nil
}

// ListFlows returns all flows for repoKey ordered by priority descending.
func (s *Store) ListFlows(ctx context.Context, repoKey string) ([]Flow, error) {
	if err := s.EnsureFlowsTable(ctx); err != nil {
		return nil, fmt.Errorf("flows: schema: %w", err)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("flows: acquire: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx, `
		SELECT flow_id, name, entry_sym, entry_file, leaf_sym,
		       member_syms, priority, community
		FROM public.code_flows
		WHERE repo_key = $1
		ORDER BY priority DESC, name ASC`,
		repoKey,
	)
	if err != nil {
		return nil, fmt.Errorf("flows: query: %w", err)
	}
	defer rows.Close()

	var out []Flow
	for rows.Next() {
		var f Flow
		f.MemberSyms = []string{}
		if err := rows.Scan(
			&f.FlowID, &f.Name, &f.EntrySym, &f.EntryFile, &f.LeafSym,
			&f.MemberSyms, &f.Priority, &f.Community,
		); err != nil {
			return nil, fmt.Errorf("flows: scan: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flows: rows: %w", err)
	}
	return out, nil
}

// insertFlowsBatch inserts flows using pgx CopyFromRows for efficiency.
// Falls back to individual INSERT statements if CopyFrom is unavailable.
func insertFlowsBatch(ctx context.Context, tx pgx.Tx, repoKey string, flows []Flow) error {
	// Build rows for CopyFrom.
	rows := make([][]any, len(flows))
	for i, f := range flows {
		rows[i] = []any{
			repoKey,
			f.FlowID,
			f.Name,
			f.EntrySym,
			f.EntryFile,
			f.LeafSym,
			f.MemberSyms,
			f.Priority,
			f.Community,
		}
	}

	cols := []string{
		"repo_key", "flow_id", "name", "entry_sym", "entry_file",
		"leaf_sym", "member_syms", "priority", "community",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"public", "code_flows"},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		// CopyFrom failed — fall back to multi-row VALUES insert.
		return insertFlowsFallback(ctx, tx, repoKey, flows)
	}
	return nil
}

// insertFlowsFallback inserts flows using a multi-row VALUES clause.
// Used when CopyFrom is unavailable (e.g. pgBouncer in transaction mode).
func insertFlowsFallback(ctx context.Context, tx pgx.Tx, repoKey string, flows []Flow) error {
	const colCount = 9
	args := make([]any, 0, len(flows)*colCount)
	var sb strings.Builder
	sb.WriteString(`INSERT INTO public.code_flows
		(repo_key, flow_id, name, entry_sym, entry_file, leaf_sym, member_syms, priority, community)
		VALUES `)

	for i, f := range flows {
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i*colCount + 1
		fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base, base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		args = append(args,
			repoKey, f.FlowID, f.Name, f.EntrySym, f.EntryFile,
			f.LeafSym, f.MemberSyms, f.Priority, f.Community,
		)
	}

	if _, err := tx.Exec(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("flows fallback insert: %w", err)
	}
	return nil
}
