package codegraph

import (
	"context"
	"fmt"
	"log/slog"
)

// buildSnapshotFromAGE reads the current graph state from AGE and builds a Snapshot.
func buildSnapshotFromAGE(ctx context.Context, store *Store, graphName string) (Snapshot, error) {
	symRows, err := store.ExecCypher(ctx, graphName,
		"MATCH (s:Symbol) RETURN s.name, s.kind, s.file, s.community, s.complexity", 5)
	if err != nil {
		if IsGraphMissingError(err) {
			store.existsCache.Forget(graphName)
			recordGraphMissing("snapshot_age")
			slog.Debug("codegraph: buildSnapshotFromAGE: graph absent — returning empty snapshot",
				slog.String("graph", graphName))
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("read symbols: %w", err)
	}

	var syms []SnapshotSymbol
	for _, row := range symRows {
		if len(row) < 5 {
			continue
		}
		syms = append(syms, SnapshotSymbol{
			Name:       stripQuotes(row[0]),
			Kind:       stripQuotes(row[1]),
			File:       stripQuotes(row[2]),
			Community:  atoiSafe(row[3]),
			Complexity: atoiSafe(row[4]),
		})
	}

	edgeRows, err := store.ExecCypher(ctx, graphName,
		"MATCH (a:Symbol)-[r]->(b:Symbol) RETURN a.name + ':' + a.file, b.name + ':' + b.file, type(r)", 3)
	if err != nil {
		if IsGraphMissingError(err) {
			// existsCache already Forgotten above if symbols query also failed;
			// safe to call again — Forget is idempotent.
			store.existsCache.Forget(graphName)
			recordGraphMissing("snapshot_age")
			slog.Debug("codegraph: buildSnapshotFromAGE: graph absent on edges query — returning partial snapshot",
				slog.String("graph", graphName))
			return Snapshot{Symbols: syms}, nil
		}
		return Snapshot{}, fmt.Errorf("read edges: %w", err)
	}

	var edges []SnapshotEdge
	for _, row := range edgeRows {
		if len(row) < 3 {
			continue
		}
		label := stripQuotes(row[2])
		if !semanticEdgeLabels[label] {
			continue
		}
		edges = append(edges, SnapshotEdge{
			From:  stripQuotes(row[0]),
			To:    stripQuotes(row[1]),
			Label: label,
		})
	}

	return Snapshot{Symbols: syms, Edges: edges}, nil
}

// SnapshotBeforeRebuild captures the current graph state before a rebuild.
// Non-fatal: failures log warnings but don't block the rebuild.
// Exported for use by tool handlers (e.g. refresh=true path).
func SnapshotBeforeRebuild(ctx context.Context, store *Store, repoKey, graphName string) {
	snap, err := buildSnapshotFromAGE(ctx, store, graphName)
	if err != nil {
		slog.Warn("codegraph: snapshot before rebuild failed", slog.Any("error", err))
		return
	}
	if len(snap.Symbols) == 0 {
		return
	}
	if err := saveSnapshot(ctx, store, repoKey, snap); err != nil {
		slog.Warn("codegraph: save snapshot failed", slog.Any("error", err))
	}
}
