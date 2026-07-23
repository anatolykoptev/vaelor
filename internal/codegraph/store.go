package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// graphExistsCacheTTL is the positive-cache duration for graph-existence preflight
// checks. Negative results are never cached — a graph may be created at any moment.
const graphExistsCacheTTL = 30 * time.Second

// ageSetup runs per-connection AGE initialization.
// Requires AGE to be in shared_preload_libraries (verified at startup by CheckAGEPreloaded).
// Only sets the search path — LOAD is not needed when AGE is server-preloaded.
const ageSetup = `SET search_path TO ag_catalog, "$user", public`

// metaTableSQL defines the schema for tracking built code graphs.
//
// Schema-qualified to public (issue #520): EnsureGraph runs after ageSetup
// (SET search_path TO ag_catalog, "$user", public), so an UNqualified
// CREATE TABLE would resolve to ag_catalog — the first schema in search_path
// where the role has CREATE privilege — and the table would leak into
// ag_catalog, owned-by-birth by whatever role ran the DDL. Qualifying as
// public.<name> forces creation into the app schema under the app's own
// connection, so the app owns the table from birth and the staleness marker
// in code_graph_meta never freezes. Bare-name accessors (getMeta/upsertMeta)
// still resolve via the ageSetup search_path (ag_catalog, public), so
// existing prod tables that already live in ag_catalog (already healed,
// out-of-scope to move) keep working.
const metaTableSQL = `
CREATE TABLE IF NOT EXISTS public.code_graph_meta (
    repo_key      TEXT PRIMARY KEY,
    repo_path     TEXT NOT NULL,
    graph_name    TEXT NOT NULL,
    file_count    INT,
    symbol_count  INT,
    edge_count    INT,
    built_at      TIMESTAMPTZ NOT NULL,
    ttl_seconds   INT DEFAULT 3600,
    content_hash  TEXT DEFAULT ''
)`

// metaTableMigrateSQL adds the content_hash column to pre-existing
// code_graph_meta tables. Runs once at EnsureGraph time; IF NOT EXISTS
// makes it idempotent. The column stores ingest.RepoContentHash(root) at
// build time so checkCache can detect file changes within the TTL window
// (issue #592: stale graph served until TTL expires despite file changes).
const metaTableMigrateSQL = `
ALTER TABLE public.code_graph_meta ADD COLUMN IF NOT EXISTS content_hash TEXT DEFAULT ''`

// mtimeTableSQL defines the schema for tracking per-file modification times.
// Schema-qualified to public — see metaTableSQL for the leak-prevention rationale.
const mtimeTableSQL = `
CREATE TABLE IF NOT EXISTS public.code_file_mtimes (
    repo_key  TEXT NOT NULL,
    file_path TEXT NOT NULL,
    mod_time  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_key, file_path)
)`

// deadCodeScoresTableSQL defines the schema for pre-computed dead_code reranker scores.
// Schema-qualified to public — see metaTableSQL for the leak-prevention rationale.
const deadCodeScoresTableSQL = `
CREATE TABLE IF NOT EXISTS public.code_dead_code_scores (
    repo_key  TEXT NOT NULL,
    name      TEXT NOT NULL,
    file      TEXT NOT NULL,
    score     REAL NOT NULL,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_key, name, file)
)`

// Store wraps a pgxpool for Apache AGE graph operations on code repositories.
type Store struct {
	pool        *pgxpool.Pool
	ageMu       sync.Mutex
	ageState    int8 // 0 = unknown, 1 = available, -1 = unavailable
	existsCache *graphExistsCache
}

// NewStore creates a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:        pool,
		existsCache: newGraphExistsCache(graphExistsCacheTTL),
	}
}

// Pool returns the underlying connection pool (for testing and diagnostics).
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// acquireAGE acquires a pooled connection and applies the AGE search_path
// (ageSetup). The codegraph bookkeeping tables (code_graph_meta, code_file_mtimes,
// code_graph_snapshots, code_dead_code_scores) are created in the public schema
// on fresh DBs (#520: public.-qualified DDL) but may still live in ag_catalog on
// pre-existing deployments that were healed in place; ageSetup puts ag_catalog
// ahead of public in search_path so bare-name access resolves to whichever
// schema holds the table. Without ageSetup the default `"$user", public`
// search_path hides any legacy ag_catalog copy and access fails with 42P01.
// Callers must Release the returned connection. (Tables in public —
// code_health_cache, code_repo_state, code_embeddings — must NOT use this;
// plain Acquire keeps them on the default search_path where their live data
// lives.)
func (s *Store) acquireAGE(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		conn.Release()
		return nil, fmt.Errorf("age setup: %w", err)
	}
	return conn, nil
}

// CheckAGEPreloaded verifies that the AGE extension is loaded in PostgreSQL.
// Call once at service startup — before any Cypher query — to fail fast if
// shared_preload_libraries does not include 'age'. Without server-side preloading
// AGE types/operators are unavailable and all graph operations will fail.
func (s *Store) CheckAGEPreloaded(ctx context.Context) error {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_extension WHERE extname = 'age'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("age extension probe: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("AGE extension not loaded: add 'age' to shared_preload_libraries in postgresql.conf and restart postgres")
	}
	slog.Info("AGE extension confirmed preloaded")
	return nil
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
	// Disable statement_timeout for bulk operations: individual UNWIND batches
	// on large graphs can exceed the default 30s limit.
	if _, err := conn.Exec(ctx, "SET statement_timeout = 0"); err != nil {
		conn.Release()
		return nil, fmt.Errorf("set statement_timeout: %w", err)
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
	_, _ = bw.conn.Exec(ctx, "RESET statement_timeout")
	bw.conn.Release()
}

// GraphExists returns true if the named AGE graph exists.
// Used as preflight before cypher queries on read-path to avoid
// generating "graph does not exist" errors in postgres logs when
// the repo was never indexed.
func (s *Store) GraphExists(ctx context.Context, graphName string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)`,
		graphName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("graph_exists check: %w", err)
	}
	return exists, nil
}

// EnsureGraphExistsForRead is the cheap preflight for read-path callers.
// Returns ErrGraphNotIndexed without hitting AGE if the graph is known to be
// absent. Returns nil if the graph exists.
//
// Cache: positive checks are valid for graphExistsCacheTTL (30s); on cache miss
// it hits ag_catalog.ag_graph via SELECT EXISTS (cheap). Negative results are
// NOT cached — a graph may be created by IndexRepo at any moment.
func (s *Store) EnsureGraphExistsForRead(ctx context.Context, graphName string) error {
	if s.existsCache.Hit(graphName) {
		return nil
	}
	exists, err := s.GraphExists(ctx, graphName)
	if err != nil {
		return err
	}
	if !exists {
		return ErrGraphNotIndexed
	}
	s.existsCache.Mark(graphName)
	return nil
}
