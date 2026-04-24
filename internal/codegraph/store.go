package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// mtimeTableSQL defines the schema for tracking per-file modification times.
const mtimeTableSQL = `
CREATE TABLE IF NOT EXISTS code_file_mtimes (
    repo_key  TEXT NOT NULL,
    file_path TEXT NOT NULL,
    mod_time  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_key, file_path)
)`


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


// CypherWriter is implemented by *Store and *BulkWriter.
// insertBatches and insertEdgeBatches accept this interface so they can
// work with either a pooled connection (Store) or a dedicated bulk session (BulkWriter).
type CypherWriter interface {
	ExecCypherWrite(ctx context.Context, graph, cypher string) error
}

// BulkWriter holds a dedicated pool connection with synchronous_commit=off
// for the duration of a graph insert operation.
// Benchmarks show 5x speedup over per-call pool acquire/release.
// Close() MUST always be called (defer it).
type BulkWriter struct {
	conn *pgxpool.Conn
}

// NewBulkWriter acquires a connection and sets synchronous_commit=off.
// Returns (nil, nil) on acquire failure — callers should fall back to Store.
func (s *Store) NewBulkWriter(ctx context.Context) (*BulkWriter, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire bulk connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "SET synchronous_commit = off"); err != nil {
		conn.Release()
		return nil, fmt.Errorf("set synchronous_commit: %w", err)
	}
	return &BulkWriter{conn: conn}, nil
}

// ExecCypherWrite runs a write Cypher on the held connection (implements CypherWriter).
func (bw *BulkWriter) ExecCypherWrite(ctx context.Context, graph, cypher string) error {
	if err := validateGraphName(graph); err != nil {
		return err
	}
	if _, err := bw.conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("AGE setup: %w", err)
	}
	tag := cypherDollarQuote(cypher)
	sql := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', %s %s %s) AS (v ag_catalog.agtype)`,
		graph, tag, cypher, tag)

	rows, err := bw.conn.Query(ctx, sql)
	if err != nil {
		slog.Error("bulk cypher write failed", slog.Any("error", err), slog.Int("sql_len", len(sql)))
		return fmt.Errorf("cypher write: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("cypher write drain: %w", err)
	}
	return nil
}

// Close resets synchronous_commit and returns the connection to the pool.
func (bw *BulkWriter) Close(ctx context.Context) {
	_, _ = bw.conn.Exec(ctx, "RESET synchronous_commit")
	bw.conn.Release()
}
