package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/strutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ageExpandSetup sets the search path for AGE Cypher queries. LOCAL-scoped to
// the enclosing transaction so nothing leaks into the pooled connection's next
// acquirer. Requires AGE in shared_preload_libraries (verified at startup by
// codegraph.Store.CheckAGEPreloaded).
const ageExpandSetup = `SET LOCAL search_path TO ag_catalog, "$user", public`

// graphRowCols is the number of columns returned by graph neighbor queries (name, file, kind).
const graphRowCols = 3

// cypherExecFn is the test seam over the AGE Cypher executor. Production leaves it
// nil and execCypherN runs the pool-backed path (execCypherNPool); unit tests inject
// a deterministic fake so the exact-vs-heuristic dispatcher, the IMPLEMENTS
// presence-probe, and the 2-hop result parsing can be exercised without a live AGE DB.
type cypherExecFn func(ctx context.Context, graphName, cypher, colDefs string) [][]string

// Expander enriches semantic search results with 1-hop CALLS neighbors from Apache AGE.
type Expander struct {
	pool *pgxpool.Pool
	// execCypherFn, when non-nil, replaces the pool-backed Cypher executor. It is
	// the injection point for unit tests; production wiring (NewExpander) leaves it
	// nil so execCypherN uses the real pool path.
	execCypherFn cypherExecFn
}

// NewExpander creates an Expander backed by the given connection pool.
func NewExpander(pool *pgxpool.Pool) *Expander {
	return &Expander{pool: pool}
}

// Expand queries the AGE graph for 1-hop CALLS neighbors of the symbols in results.
// Returns at most maxExtra additional SearchResult entries (source="graph") not already
// present in the input. If the graph does not exist or any query fails, returns nil gracefully.
func (e *Expander) Expand(ctx context.Context, graphName string, results []SearchResult, maxExtra int) []SearchResult {
	if len(results) == 0 || maxExtra <= 0 {
		return nil
	}

	// Build dedup set from existing results.
	seen := make(map[string]bool, len(results))
	names := make([]string, 0, len(results))
	for _, r := range results {
		key := r.FilePath + ":" + r.SymbolName
		seen[key] = true
		names = append(names, r.SymbolName)
	}

	nameFilter := buildNameFilter("a", names)
	nameFilterB := buildNameFilter("b", names)

	// Forward: symbols in results call these.
	fwdCypher := fmt.Sprintf(
		`MATCH (a)-[:CALLS]->(b) WHERE %s RETURN b.name, b.file, b.kind`,
		nameFilter,
	)
	// Reverse: these symbols call symbols in results.
	revCypher := fmt.Sprintf(
		`MATCH (a)-[:CALLS]->(b) WHERE %s RETURN a.name, a.file, a.kind`,
		nameFilterB,
	)

	var extra []SearchResult
	for _, cypher := range []string{fwdCypher, revCypher} {
		rows := e.execCypher(ctx, graphName, cypher)
		for _, row := range rows {
			if len(row) < graphRowCols {
				continue
			}
			name := strutil.UnquoteAgtype(row[0])
			file := strutil.UnquoteAgtype(row[1])
			kind := strutil.UnquoteAgtype(row[2])
			if name == "" || file == "" {
				continue
			}
			deduKey := file + ":" + name
			if seen[deduKey] {
				continue
			}
			seen[deduKey] = true
			extra = append(extra, SearchResult{
				FilePath:   file,
				SymbolName: name,
				SymbolKind: kind,
				Distance:   1.0, // no vector score for graph neighbors
				Source:     "graph",
			})
			if len(extra) >= maxExtra {
				return extra
			}
		}
	}
	return extra
}

// execCypher runs a read-only 3-column Cypher query against the named AGE graph.
// The AS clause is fixed to (name agtype, file agtype, kind agtype).
// Returns nil on any error (graph missing, AGE unavailable, etc.).
// For queries with a different column count, use execCypherN.
func (e *Expander) execCypher(ctx context.Context, graphName string, cypher string) [][]string {
	return e.execCypherN(ctx, graphName, cypher, "name agtype, file agtype, kind agtype")
}

// execCypherN runs a read-only Cypher query against the named AGE graph with a
// caller-supplied AS-clause column definition. The colDefs string must list all
// columns returned by the Cypher RETURN clause, e.g.
// "name agtype, file agtype, kind agtype, community agtype".
// AGE requires the AS-clause arity to match the RETURN arity exactly; a mismatch
// raises "return row and column definition list do not match" → conn.Query errors
// → nil is returned. Returns nil on any error.
//
// When execCypherFn is set (unit tests), it replaces the pool-backed path; otherwise
// execCypherNPool runs against the live AGE pool.
func (e *Expander) execCypherN(ctx context.Context, graphName, cypher, colDefs string) [][]string {
	if e.execCypherFn != nil {
		return e.execCypherFn(ctx, graphName, cypher, colDefs)
	}
	return e.execCypherNPool(ctx, graphName, cypher, colDefs)
}

// execCypherNPool is the pool-backed AGE executor. See execCypherN for the column
// contract. Returns nil on any error (graph missing, AGE unavailable, etc.).
func (e *Expander) execCypherNPool(ctx context.Context, graphName, cypher, colDefs string) [][]string {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("graph expand: acquire connection failed", slog.Any("error", err))
		return nil
	}
	defer conn.Release()

	// Read-only transaction: PostgreSQL enforces no-write server-side against
	// the parsed statement, so even a Cypher literal that broke out of its
	// quoting could not mutate the graph (#802). This guards every internally
	// generated query without relying on a client-side lexical scan to stay in
	// lock-step with AGE's lexer.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		slog.Debug("graph expand: begin tx failed", slog.Any("error", err))
		return nil
	}
	defer func() { _ = tx.Rollback(ctx) }() // read-only — rollback is enough

	if _, err := tx.Exec(ctx, ageExpandSetup); err != nil {
		slog.Debug("graph expand: AGE setup failed", slog.Any("error", err))
		return nil
	}

	// Check if graph exists before querying to avoid postgres ERROR logs.
	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)`,
		graphName,
	).Scan(&exists)
	if err != nil || !exists {
		slog.Debug("graph expand: graph not found", slog.String("graph", graphName))
		return nil
	}

	sql, ok := wrapCypherSQL(graphName, cypher, colDefs)
	if !ok {
		slog.Debug("graph expand: no usable dollar-quote tag", slog.String("graph", graphName))
		return nil
	}
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		slog.Debug("graph expand: cypher query failed",
			slog.String("graph", graphName), slog.Any("error", err))
		return nil
	}
	defer rows.Close()

	var result [][]string
	for rows.Next() {
		vals, scanErr := rows.Values()
		if scanErr != nil {
			continue
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		slog.Debug("graph expand: row iteration failed",
			slog.String("graph", graphName), slog.Any("error", err))
		return nil
	}
	return result
}

// buildNameFilter builds a Cypher WHERE condition inlining names as OR-joined literals.
// variable is the node alias (e.g. "a" or "b").
// AGE does not support parameterized arrays in Cypher, so names must be inlined.
func buildNameFilter(variable string, names []string) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s.name = '%s'", variable, escapeCypherName(n)))
	}
	return strings.Join(parts, " OR ")
}

// wrapCypherSQL renders the SQL that carries a Cypher statement to
// ag_catalog.cypher. Delegate of strutil.WrapCypherSQL — the single assembler
// shared with codegraph (#808).
func wrapCypherSQL(graphName, cypher, colDefs string) (string, bool) {
	return strutil.WrapCypherSQL(graphName, cypher, colDefs)
}
