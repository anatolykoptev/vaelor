package compare

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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

	// Serial execution — pgxpool has only ~4 connections by default.
	// Two repos run in parallel (called from enrich.go), so 2 concurrent
	// queries is safe. More concurrency exhausts the pool.
	m.PackageCount = queryPackageCount(ctx, store, graph)
	m.CrossPkgCallRatio = queryCrossPkgRatio(ctx, store, graph)
	m.InterfaceRatio = queryInterfaceRatio(ctx, store, graph)
	m.GodPackages = queryGodPackages(ctx, store, graph)
	m.CircularDeps = queryCircularDeps(ctx, store, graph)
	// MaxCallDepth is expensive (variable-length path probing) — run last.
	m.MaxCallDepth = queryMaxCallDepth(ctx, store, graph)

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
	// AGE limitation: no CASE inside count(). Use two separate queries.
	// Total CALLS edges:
	totalRows, err := store.ExecCypher(ctx, graph,
		`MATCH (a:Symbol)-[:CALLS]->(b:Symbol) RETURN count(*)`, 1)
	if err != nil || len(totalRows) == 0 {
		return 0
	}
	total, _ := strconv.Atoi(totalRows[0][0])
	if total == 0 {
		return 0
	}

	// Cross-file CALLS edges (proxy for cross-package):
	crossRows, err := store.ExecCypher(ctx, graph,
		`MATCH (a:Symbol)-[:CALLS]->(b:Symbol) WHERE a.file <> b.file RETURN count(*)`, 1)
	if err != nil || len(crossRows) == 0 {
		return 0
	}
	cross, _ := strconv.Atoi(crossRows[0][0])
	return float64(cross) / float64(total)
}

// maxProbedCallDepth is the deepest call chain length we probe.
const maxProbedCallDepth = 10

func queryMaxCallDepth(ctx context.Context, store *codegraph.Store, graph string) int {
	// AGE limitation: max(length(p)) doesn't work in aggregate.
	// Strategy: binary search for deepest non-empty path length.
	// Fixed-length paths (*N..N) work reliably in AGE.
	// Worst case: log2(10) ≈ 4 queries instead of linear 10.
	lo, hi, best := 1, maxProbedCallDepth, 0
	for lo <= hi {
		mid := (lo + hi) / 2
		cypher := fmt.Sprintf(
			`MATCH (a:Symbol)-[:CALLS*%d..%d]->(b:Symbol) RETURN count(*)`, mid, mid)
		rows, err := store.ExecCypher(ctx, graph, cypher, 1)
		if err != nil || len(rows) == 0 {
			// Query failed — try shallower to avoid timeout.
			hi = mid - 1
			continue
		}
		n, _ := strconv.Atoi(rows[0][0])
		if n > 0 {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

func queryInterfaceRatio(ctx context.Context, store *codegraph.Store, graph string) float64 {
	// IMPLEMENTS edges don't exist in this graph. Use abstraction proxy:
	// ratio of interface symbols to (struct + interface) symbols.
	// Higher ratio = more abstracted code.
	ifaceRows, err := store.ExecCypher(ctx, graph,
		`MATCH (s:Symbol) WHERE s.kind = 'interface' RETURN count(s)`, 1)
	if err != nil || len(ifaceRows) == 0 {
		return 0
	}
	ifaces, _ := strconv.Atoi(ifaceRows[0][0])

	structRows, err := store.ExecCypher(ctx, graph,
		`MATCH (s:Symbol) WHERE s.kind = 'struct' RETURN count(s)`, 1)
	if err != nil || len(structRows) == 0 {
		return 0
	}
	structs, _ := strconv.Atoi(structRows[0][0])

	total := ifaces + structs
	if total == 0 {
		return 0
	}
	return float64(ifaces) / float64(total)
}

func queryGodPackages(ctx context.Context, store *codegraph.Store, graph string) []GodPackage {
	// IMPORTS edges go File→Package, not Package→Package.
	// Derive package-level importers via: Package-[:CONTAINS]->File-[:IMPORTS]->Package.
	// Use DISTINCT p1 to count unique importing packages.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p1:Package)-[:CONTAINS]->(f:File)-[:IMPORTS]->(p2:Package)
		 WHERE p1.name <> p2.name
		 RETURN p2.name, count(DISTINCT p1) AS importers
		 ORDER BY importers DESC
		 LIMIT 50`, 2)
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
		if n < godPackageThreshold {
			continue
		}
		// Strip quotes from AGE string format.
		name := strings.Trim(row[0], `"`)
		result = append(result, GodPackage{Name: name, Importers: n})
	}
	return result
}

func queryCircularDeps(ctx context.Context, store *codegraph.Store, graph string) []CircularDep {
	// AGE limitation: id(a) < id(b) doesn't work for deduplication.
	// Strategy: fetch all package-level imports, build adjacency in Go, find 2-cycles.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p1:Package)-[:CONTAINS]->(f:File)-[:IMPORTS]->(p2:Package)
		 WHERE p1.name <> p2.name
		 RETURN DISTINCT p1.name, p2.name`, 2)
	if err != nil {
		slog.Debug("archgraph: package imports query failed", "err", err)
		return nil
	}

	// Build adjacency: A imports B.
	imports := make(map[string]map[string]bool)
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		a := strings.Trim(row[0], `"`)
		b := strings.Trim(row[1], `"`)
		if imports[a] == nil {
			imports[a] = make(map[string]bool)
		}
		imports[a][b] = true
	}

	// Find 2-cycles (A→B and B→A). Deduplicate by alphabetical ordering.
	seen := make(map[string]bool)
	var result []CircularDep
	for a, deps := range imports {
		for b := range deps {
			if imports[b] != nil && imports[b][a] {
				// Canonical key: lexicographically smaller first.
				key := a + "|" + b
				if a > b {
					key = b + "|" + a
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, CircularDep{PackageA: a, PackageB: b})
			}
		}
	}
	return result
}
