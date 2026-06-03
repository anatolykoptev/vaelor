package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ageExpandSetup sets the search path for AGE Cypher queries.
// Requires AGE in shared_preload_libraries (verified at startup by codegraph.Store.CheckAGEPreloaded).
const ageExpandSetup = `SET search_path TO ag_catalog, "$user", public`

// graphRowCols is the number of columns returned by graph neighbor queries (name, file, kind).
const graphRowCols = 3

// Expander enriches semantic search results with 1-hop CALLS neighbors from Apache AGE.
type Expander struct {
	pool *pgxpool.Pool
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
			name := stripAgtypeQuotes(row[0])
			file := stripAgtypeQuotes(row[1])
			kind := stripAgtypeQuotes(row[2])
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
func (e *Expander) execCypherN(ctx context.Context, graphName, cypher, colDefs string) [][]string {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("graph expand: acquire connection failed", slog.Any("error", err))
		return nil
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageExpandSetup); err != nil {
		slog.Debug("graph expand: AGE setup failed", slog.Any("error", err))
		return nil
	}

	// Check if graph exists before querying to avoid postgres ERROR logs.
	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)`,
		graphName,
	).Scan(&exists)
	if err != nil || !exists {
		slog.Debug("graph expand: graph not found", slog.String("graph", graphName))
		return nil
	}

	sql := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) AS (%s)`,
		graphName, cypher, colDefs,
	)
	rows, err := conn.Query(ctx, sql)
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
	return result
}

// buildNameFilter builds a Cypher WHERE condition inlining names as OR-joined literals.
// variable is the node alias (e.g. "a" or "b").
// AGE does not support parameterized arrays in Cypher, so names must be inlined.
func buildNameFilter(variable string, names []string) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		escaped := strings.ReplaceAll(n, "'", "\\'")
		parts = append(parts, fmt.Sprintf("%s.name = '%s'", variable, escaped))
	}
	return strings.Join(parts, " OR ")
}

// stripAgtypeQuotes removes the surrounding double-quotes that AGE wraps string values in.
// AGE returns string agtypes as "foo" (JSON-quoted). Returns the bare value.
func stripAgtypeQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
