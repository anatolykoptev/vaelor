package codegraph

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

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

// mtimeTableSQL defines the schema for tracking per-file modification times.
const mtimeTableSQL = `
CREATE TABLE IF NOT EXISTS code_file_mtimes (
    repo_key  TEXT NOT NULL,
    file_path TEXT NOT NULL,
    mod_time  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_key, file_path)
)`

// reWriteOp matches Cypher write keywords — used to reject writes in ExecCypher.
var reWriteOp = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|DETACH)\b`)

// Store wraps a pgxpool for Apache AGE graph operations on code repositories.
type Store struct {
	pool     *pgxpool.Pool
	ageMu    sync.Mutex
	ageState int8 // 0 = unknown, 1 = available, -1 = unavailable
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
// A positive result is cached permanently. Failures are retried on subsequent calls
// so that a temporary DB outage at startup does not permanently disable AGE.
func (s *Store) HasAGE(ctx context.Context) bool {
	s.ageMu.Lock()
	defer s.ageMu.Unlock()

	if s.ageState != 0 {
		return s.ageState == 1
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		slog.Warn("AGE check: failed to acquire connection", slog.Any("error", err))
		return false // leave ageState=0 so next call retries
	}
	defer conn.Release()

	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'age')`,
	).Scan(&exists)
	if err != nil {
		slog.Warn("AGE check: query failed", slog.Any("error", err))
		return false // leave ageState=0 so next call retries
	}

	if exists {
		s.ageState = 1
	} else {
		s.ageState = -1
	}
	slog.Info("AGE availability checked", slog.Bool("available", exists))
	return exists
}

// ExecCypher executes a read-only Cypher query against the named graph and returns
// string rows. cols is the number of projected columns in the query.
// Returns an error if the Cypher contains write operations.
func (s *Store) ExecCypher(ctx context.Context, graph, cypher string, cols int) ([][]string, error) {
	if err := validateGraphName(graph); err != nil {
		return nil, err
	}
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
	tag := cypherDollarQuote(cypher)
	sql := fmt.Sprintf(`SELECT * FROM ag_catalog.cypher('%s', %s %s %s) AS (%s)`,
		graph, tag, cypher, tag, colDefs)

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
	if err := validateGraphName(graph); err != nil {
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

	// Write statements must project at least one column for cypher() to accept them.
	tag := cypherDollarQuote(cypher)
	sql := fmt.Sprintf(`SELECT * FROM ag_catalog.cypher('%s', %s %s %s) AS (v ag_catalog.agtype)`,
		graph, tag, cypher, tag)

	slog.Debug("ExecCypherWrite", slog.Int("sql_len", len(sql)))

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		slog.Error("cypher write failed", slog.Any("error", err),
			slog.Int("sql_len", len(sql)),
			slog.String("cypher_head", truncate(cypher, 200))) //nolint:mnd // debug truncation
		return fmt.Errorf("cypher write: %w", err)
	}
	defer rows.Close()

	// Drain rows — write statements typically return the affected vertices/edges.
	for rows.Next() {
		// intentionally empty: we only care about side effects
	}

	if err := rows.Err(); err != nil {
		slog.Error("cypher write drain failed", slog.Any("error", err),
			slog.Int("sql_len", len(sql)))
		return fmt.Errorf("cypher write drain: %w", err)
	}

	return nil
}

// truncate returns the first n runes of s, or s if shorter.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

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

	_, _ = conn.Exec(ctx, `DELETE FROM code_file_mtimes WHERE repo_key = $1`, repoKey)

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
	s = strings.ReplaceAll(s, "\x00", "") // strip null bytes
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// reGraphName validates graph names: only lowercase alphanumeric and underscores.
var reGraphName = regexp.MustCompile(`^[a-z0-9_]+$`)

// validateGraphName returns an error if the name contains unsafe characters.
func validateGraphName(name string) error {
	if !reGraphName.MatchString(name) {
		return fmt.Errorf("invalid graph name %q: must match [a-z0-9_]+", name)
	}
	return nil
}

// cypherDollarQuote returns a dollar-quoting tag that does not appear in the
// Cypher body. PostgreSQL dollar-quoting: $tag$...$tag$.
func cypherDollarQuote(cypher string) string {
	tag := "$cq$"
	for strings.Contains(cypher, tag) {
		tag = fmt.Sprintf("$cq%d$", rand.IntN(99999)) //nolint:mnd,gosec // random suffix, not crypto
	}
	return tag
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

// UpsertFileMtimes stores file modification times for a repo.
// It replaces all existing entries for the given repoKey.
func (s *Store) UpsertFileMtimes(ctx context.Context, repoKey string, mtimes map[string]time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, "DELETE FROM code_file_mtimes WHERE repo_key = $1", repoKey)
	if err != nil {
		return fmt.Errorf("delete old mtimes: %w", err)
	}
	for path, mtime := range mtimes {
		_, err = conn.Exec(ctx,
			"INSERT INTO code_file_mtimes (repo_key, file_path, mod_time) VALUES ($1, $2, $3)",
			repoKey, path, mtime)
		if err != nil {
			return fmt.Errorf("insert mtime: %w", err)
		}
	}
	return nil
}

// GetFileMtimes retrieves stored file modification times for a repo.
func (s *Store) GetFileMtimes(ctx context.Context, repoKey string) (map[string]time.Time, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		"SELECT file_path, mod_time FROM code_file_mtimes WHERE repo_key = $1", repoKey)
	if err != nil {
		return nil, fmt.Errorf("query mtimes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var path string
		var modTime time.Time
		if err := rows.Scan(&path, &modTime); err != nil {
			return nil, fmt.Errorf("scan mtime: %w", err)
		}
		result[path] = modTime
	}
	return result, rows.Err()
}

// DeleteFileMtimes removes stored file modification times for a repo.
func (s *Store) DeleteFileMtimes(ctx context.Context, repoKey string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM code_file_mtimes WHERE repo_key = $1", repoKey)
	return err
}
