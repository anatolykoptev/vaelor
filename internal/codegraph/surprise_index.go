// Package codegraph — surprise_index.go
//
// Two-phase surprise persistence. Run IndexSurpriseEdges first to score and
// write r.surprise on every CALLS edge; then run IndexSurpriseNodes to
// aggregate per-symbol scores.  Both functions are safe to re-run: they
// overwrite previously persisted scores rather than accumulating them.
package codegraph

import (
	"context"
	"fmt"
	"log/slog"
)

// maxSurpriseScore mirrors scoreSurprise's theoretical maximum.
// Components: cross-package(2) + cross-community(1) + peripheral→hub(1)
// + pagerank-gap(1) + cross-file(1) = 6.
// Used to normalise persisted surprise into the 0..1 range graphx expects.
const maxSurpriseScore = 6

// maxEdgeScan caps how many CALLS edges are fetched in one indexing pass.
// Guards against runaway memory on very large repos (>20k call edges is rare;
// if it happens, only the top 20k edges by AGE scan order are scored).
const maxEdgeScan = 20000

// IndexSurpriseEdges fetches every CALLS edge joined with endpoint pagerank,
// community, file, and degree; scores each via scoreSurprise; and writes
// r.surprise = score on edges with score > 0.  Edges scoring 0 are skipped
// to keep writes bounded.
//
// Safe to run multiple times — overwrites stale scores.  Call BEFORE
// IndexSurpriseNodes so that node aggregation sees fresh edge values.
func IndexSurpriseEdges(ctx context.Context, store *Store, graphName string) error {
	slog.Info("codegraph: IndexSurpriseEdges starting", slog.String("graph", graphName))

	// Phase 1: fetch all CALLS edges with endpoint analytics columns.
	edgeCypher := fmt.Sprintf(
		`MATCH (a:Symbol)-[r:CALLS]->(b:Symbol) `+
			`RETURN a.name, a.file, a.community, a.pagerank, `+
			`b.name, b.file, b.community, b.pagerank `+
			`LIMIT %d`,
		maxEdgeScan,
	)
	edgeRows, err := store.ExecCypher(ctx, graphName, edgeCypher, 8) //nolint:mnd // 8 projected cols
	if err != nil {
		if IsGraphMissingError(err) {
			store.existsCache.Forget(graphName)
			recordGraphMissing("surprise_edges")
			slog.Debug("codegraph: IndexSurpriseEdges: graph absent (write-path race) — skipping",
				slog.String("graph", graphName))
			return nil
		}
		return fmt.Errorf("IndexSurpriseEdges fetch edges: %w", err)
	}
	slog.Info("codegraph: IndexSurpriseEdges fetched edges", slog.Int("count", len(edgeRows)))

	// Phase 2: fetch degree map (name → total call degree).
	degCypher := `MATCH (s:Symbol)-[:CALLS]-() RETURN s.name, count(*)`
	degRows, err := store.ExecCypher(ctx, graphName, degCypher, 2) //nolint:mnd // 2 projected cols
	if err != nil {
		if IsGraphMissingError(err) {
			// existsCache already Forgotten after Phase 1 succeeded, so graph
			// was dropped in the window between the two queries (rare race).
			store.existsCache.Forget(graphName)
			recordGraphMissing("surprise_edges")
			slog.Debug("codegraph: IndexSurpriseEdges: graph absent on degree query — skipping",
				slog.String("graph", graphName))
			return nil
		}
		// Degree info is optional — fall back to 0 for all, which just disables
		// the peripheral→hub heuristic without failing the whole pass.
		slog.Warn("codegraph: IndexSurpriseEdges degree query failed (non-fatal), proceeding without degree data",
			slog.Any("error", err))
		degRows = nil
	}

	degrees := make(map[string]int, len(degRows))
	for _, row := range degRows {
		if len(row) < 2 { //nolint:mnd // guard: need name + count
			continue
		}
		name := trimQuotes(row[0])
		degrees[name] = atoiSafe(row[1])
	}

	// Phase 3: score edges and flush in batches of ≤batchSize writes.
	var (
		scored  int
		written int
	)

	for _, row := range edgeRows {
		if len(row) < 8 { //nolint:mnd // guard: need all 8 cols
			continue
		}
		e := surpriseEdge{
			FromName:      trimQuotes(row[0]),
			FromFile:      trimQuotes(row[1]),
			ToName:        trimQuotes(row[4]),
			ToFile:        trimQuotes(row[5]),
			EdgeLabel:     "CALLS",
			FromCommunity: atoiSafe(row[2]),
			ToCommunity:   atoiSafe(row[6]),
			FromPageRank:  atofSafe(row[3]),
			ToPageRank:    atofSafe(row[7]),
		}
		e.FromPkg = pkgFromFile(e.FromFile)
		e.ToPkg = pkgFromFile(e.ToFile)
		e.FromDegree = degrees[e.FromName]
		e.ToDegree = degrees[e.ToName]

		score, _ := scoreSurprise(e)
		if score == 0 {
			continue
		}
		scored++

		setCypher := fmt.Sprintf(
			`MATCH (a:Symbol {name: '%s', file: '%s'})-[r:CALLS]->(b:Symbol {name: '%s', file: '%s'}) `+
				`SET r.surprise = %d`,
			escapeCypher(e.FromName), escapeCypher(e.FromFile),
			escapeCypher(e.ToName), escapeCypher(e.ToFile),
			score,
		)
		if werr := store.ExecCypherWrite(ctx, graphName, setCypher); werr != nil {
			slog.Warn("codegraph: IndexSurpriseEdges write failed (non-fatal)",
				slog.String("from", e.FromName),
				slog.String("to", e.ToName),
				slog.Any("error", werr))
			continue
		}
		written++
	}

	slog.Info("codegraph: IndexSurpriseEdges done",
		slog.Int("edges_scored", scored),
		slog.Int("edges_written", written))
	return nil
}

// IndexSurpriseNodes aggregates: for each Symbol, finds the maximum surprise
// score across all adjacent CALLS edges (incoming or outgoing), normalises to
// [0, 1] by dividing by maxSurpriseScore, and writes s.surprise = normalised.
//
// Must be called AFTER IndexSurpriseEdges so that r.surprise values are
// already persisted on edges.  Safe to re-run — overwrites stale node scores.
func IndexSurpriseNodes(ctx context.Context, store *Store, graphName string) error {
	slog.Info("codegraph: IndexSurpriseNodes starting", slog.String("graph", graphName))

	// Collect (name, file, max_edge_surprise) rows.  We use OPTIONAL MATCH so
	// isolated symbols (no CALLS edges) still appear with surprise=0.
	fetchCypher := `MATCH (s:Symbol) ` +
		`OPTIONAL MATCH (s)-[r:CALLS]-(:Symbol) ` +
		`WITH s.name AS n, s.file AS f, max(coalesce(r.surprise, 0)) AS ms ` +
		`RETURN n, f, ms`

	rows, err := store.ExecCypher(ctx, graphName, fetchCypher, 3) //nolint:mnd // 3 projected cols
	if err != nil {
		if IsGraphMissingError(err) {
			store.existsCache.Forget(graphName)
			recordGraphMissing("surprise_nodes")
			slog.Debug("codegraph: IndexSurpriseNodes: graph absent (write-path race) — skipping",
				slog.String("graph", graphName))
			return nil
		}
		return fmt.Errorf("IndexSurpriseNodes fetch: %w", err)
	}

	var (
		processed int
		written   int
	)

	for _, row := range rows {
		if len(row) < 3 { //nolint:mnd // guard: need name, file, max
			continue
		}
		name := trimQuotes(row[0])
		file := trimQuotes(row[1])
		maxScore := atofSafe(row[2])

		if name == "" {
			continue
		}
		processed++

		normalised := maxScore / maxSurpriseScore
		setCypher := fmt.Sprintf(
			`MATCH (s:Symbol {name: '%s', file: '%s'}) SET s.surprise = %.6f`,
			escapeCypher(name), escapeCypher(file), normalised,
		)
		if werr := store.ExecCypherWrite(ctx, graphName, setCypher); werr != nil {
			slog.Warn("codegraph: IndexSurpriseNodes write failed (non-fatal)",
				slog.String("symbol", name),
				slog.Any("error", werr))
			continue
		}
		written++
	}

	slog.Info("codegraph: IndexSurpriseNodes done",
		slog.Int("symbols_processed", processed),
		slog.Int("symbols_written", written))
	return nil
}

// trimQuotes strips surrounding double-quotes that AGE agtype adds to string values.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
