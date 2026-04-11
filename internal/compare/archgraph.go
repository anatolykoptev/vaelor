package compare

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/codegraph"
)

// ArchMetrics holds architectural metrics derived from the code graph.
type ArchMetrics struct {
	PackageCount      int           `json:"packageCount"`
	CrossPkgCallRatio float64       `json:"crossPkgCallRatio"`
	MaxCallDepth      int           `json:"maxCallDepth"`
	InterfaceRatio    float64       `json:"interfaceRatio"`
	GodPackages       []GodPackage  `json:"godPackages,omitempty"`
	CircularDeps      []CircularDep `json:"circularDeps,omitempty"`
}

// GodPackage represents a package with many importers (high coupling).
type GodPackage struct {
	Name      string `json:"name"`
	Importers int    `json:"importers"`
}

// CircularDep represents a circular dependency between two packages.
type CircularDep struct {
	PackageA string `json:"packageA"`
	PackageB string `json:"packageB"`
}

const godPackageThreshold = 5 // packages with 5+ importers are considered "god packages"

// CollectArchMetrics queries the Apache AGE code graph for architectural metrics.
// Each sub-query is non-fatal: failures are logged and skipped.
func CollectArchMetrics(ctx context.Context, store *codegraph.Store, root string) *ArchMetrics {
	if store == nil {
		return nil
	}

	graph := codegraph.GraphNameFor(root)
	m := &ArchMetrics{}

	m.PackageCount = queryPackageCount(ctx, store, graph)
	m.CrossPkgCallRatio = queryCrossPkgRatio(ctx, store, graph)
	m.MaxCallDepth = queryMaxCallDepth(ctx, store, graph)
	m.InterfaceRatio = queryInterfaceRatio(ctx, store, graph)
	m.GodPackages = queryGodPackages(ctx, store, graph)
	m.CircularDeps = queryCircularDeps(ctx, store, graph)

	return m
}

func queryPackageCount(ctx context.Context, store *codegraph.Store, graph string) int {
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p:Package) RETURN count(p)`, 1)
	if err != nil {
		slog.Warn("archgraph: package count query failed", "graph", graph, "err", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(rows[0][0])
	return n
}

func queryCrossPkgRatio(ctx context.Context, store *codegraph.Store, graph string) float64 {
	// Count calls where caller and callee are in different files (proxy for cross-package).
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (a:Symbol)-[:CALLS]->(b:Symbol)
         WHERE a.file IS NOT NULL AND b.file IS NOT NULL
         RETURN count(CASE WHEN a.file <> b.file THEN 1 END), count(*)`, 2)
	if err != nil {
		slog.Debug("archgraph: cross-pkg ratio query failed", "err", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	cross, _ := strconv.Atoi(rows[0][0])
	total, _ := strconv.Atoi(rows[0][1])
	if total == 0 {
		return 0
	}
	return float64(cross) / float64(total)
}

func queryMaxCallDepth(ctx context.Context, store *codegraph.Store, graph string) int {
	// Bounded variable-length path prevents combinatorial explosion.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH p=(a:Symbol)-[:CALLS*1..10]->(b:Symbol)
         WHERE NOT (b)-[:CALLS]->()
         RETURN max(length(p))`, 1)
	if err != nil {
		slog.Debug("archgraph: max call depth query failed", "err", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(rows[0][0])
	return n
}

func queryInterfaceRatio(ctx context.Context, store *codegraph.Store, graph string) float64 {
	// Ratio of types that implement interfaces vs total types.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (s:Symbol) WHERE s.kind = 'type' OR s.kind = 'interface'
         RETURN count(CASE WHEN (s)-[:IMPLEMENTS]->() THEN 1 END), count(s)`, 2)
	if err != nil {
		slog.Debug("archgraph: interface ratio query failed", "err", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	impl, _ := strconv.Atoi(rows[0][0])
	total, _ := strconv.Atoi(rows[0][1])
	if total == 0 {
		return 0
	}
	return float64(impl) / float64(total)
}

func queryGodPackages(ctx context.Context, store *codegraph.Store, graph string) []GodPackage {
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (a:Package)-[:IMPORTS]->(b:Package)
         WITH b.name AS pkg, count(a) AS importers
         WHERE importers >= 5
         RETURN pkg, importers
         ORDER BY importers DESC`, 2)
	if err != nil {
		slog.Debug("archgraph: god packages query failed", "err", err)
		return nil
	}
	var result []GodPackage
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		n, _ := strconv.Atoi(row[1])
		result = append(result, GodPackage{Name: row[0], Importers: n})
	}
	return result
}

func queryCircularDeps(ctx context.Context, store *codegraph.Store, graph string) []CircularDep {
	// id(a) < id(b) deduplicates — each pair appears once.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (a:Package)-[:IMPORTS]->(b:Package)-[:IMPORTS]->(a)
         WHERE id(a) < id(b)
         RETURN a.name, b.name`, 2)
	if err != nil {
		slog.Debug("archgraph: circular deps query failed", "err", err)
		return nil
	}
	var result []CircularDep
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		result = append(result, CircularDep{PackageA: row[0], PackageB: row[1]})
	}
	return result
}
