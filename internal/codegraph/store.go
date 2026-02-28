package codegraph

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ageSetup runs per-connection AGE initialization.
// LOAD 'age' must be called on each connection before using AGE types/operators.
const ageSetup = `LOAD 'age'; SET search_path TO ag_catalog, "$user", public`

// metaTableSQL defines the schema for tracking built code graphs.
const metaTableSQL = `
CREATE TABLE IF NOT EXISTS code_graph_meta (
    repo_key     TEXT PRIMARY KEY,
    repo_path    TEXT NOT NULL,
    graph_name   TEXT NOT NULL,
    file_count   INT,
    symbol_count INT,
    edge_count   INT,
    built_at     TIMESTAMPTZ NOT NULL,
    ttl_seconds  INT DEFAULT 3600
)`

// reWriteOp matches Cypher write keywords — used to reject writes in ExecCypher.
var reWriteOp = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|DETACH)\b`)

// Store wraps a pgxpool for Apache AGE graph operations on code repositories.
type Store struct {
	pool    *pgxpool.Pool
	ageOnce sync.Once
	ageAvail bool
}

// NewStore creates a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool returns the underlying connection pool (for testing and diagnostics).
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// HasAGE checks if Apache AGE extension is available in this PostgreSQL instance.
// The result is cached after the first check.
func (s *Store) HasAGE(ctx context.Context) bool {
	s.ageOnce.Do(func() {
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			slog.Warn("AGE check: failed to acquire connection", slog.Any("error", err))
			return
		}
		defer conn.Release()

		var exists bool
		err = conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'age')`,
		).Scan(&exists)
		if err != nil {
			slog.Warn("AGE check: query failed", slog.Any("error", err))
			return
		}
		s.ageAvail = exists
		slog.Info("AGE availability checked", slog.Bool("available", exists))
	})
	return s.ageAvail
}

// ExecCypher executes a read-only Cypher query against the named graph and returns
// string rows. cols is the number of projected columns in the query.
// Returns an error if the Cypher contains write operations.
func (s *Store) ExecCypher(ctx context.Context, graph, cypher string, cols int) ([][]string, error) {
	if !isReadOnly(cypher) {
		return nil, errors.New("ExecCypher: write operation detected in Cypher — use ExecCypherWrite")
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return nil, fmt.Errorf("AGE setup: %w", err)
	}

	colDefs := buildColDefs(cols)
	sql := fmt.Sprintf(`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) AS (%s)`,
		graph, cypher, colDefs)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("cypher query: %w", err)
	}
	defer rows.Close()

	var result [][]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration: %w", err)
	}

	return result, nil
}

// ExecCypherWrite executes a write Cypher statement (MERGE/CREATE/SET/DELETE) and
// drains any returned rows without collecting them.
func (s *Store) ExecCypherWrite(ctx context.Context, graph, cypher string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}

	// Write statements must project at least one column for cypher() to accept them.
	sql := fmt.Sprintf(`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) AS (v ag_catalog.agtype)`,
		graph, cypher)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("cypher write: %w", err)
	}
	defer rows.Close()

	// Drain rows — write statements typically return the affected vertices/edges.
	for rows.Next() {
		// intentionally empty: we only care about side effects
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("cypher write drain: %w", err)
	}

	return nil
}

// EnsureGraph creates the AGE graph if it does not exist and ensures the
// code_graph_meta bookkeeping table is present.
func (s *Store) EnsureGraph(ctx context.Context, name string) error {
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

	return nil
}

// DropGraph drops the AGE graph and removes the meta row for the given repoKey.
func (s *Store) DropGraph(ctx context.Context, name, repoKey string) error {
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

	return nil
}

// GraphNameFor returns the deterministic AGE graph name for the given repo path.
// Exported for callers that need the name without a Store instance.
func GraphNameFor(repoPath string) string {
	return graphName(repoPath)
}

// graphName produces a stable graph name from a repo path using FNV-32a.
// Format: code_<8-char-hex>, e.g. "code_a3f2b1c0".
func graphName(repoPath string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoPath))
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], h.Sum32())
	return fmt.Sprintf("code_%08x", buf)
}

// escapeCypher escapes a string for safe use in a single-quoted Cypher literal.
// Prevents Cypher injection by escaping backslash, quotes, backtick, and control characters.
func escapeCypher(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "\x00", "") // strip null bytes
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// isReadOnly returns true if cypher contains no write operations.
func isReadOnly(cypher string) bool {
	return !reWriteOp.MatchString(cypher)
}

// buildColDefs returns "c0 ag_catalog.agtype, c1 ag_catalog.agtype, ..." for n columns.
func buildColDefs(n int) string {
	if n <= 0 {
		n = 1
	}
	parts := make([]string, n)
	for i := range n {
		parts[i] = fmt.Sprintf("c%d ag_catalog.agtype", i)
	}
	return strings.Join(parts, ", ")
}
