package compare

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codegraph"
)

func queryPackageCount(ctx context.Context, store *codegraph.Store, graph string) int {
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p:Package) RETURN count(p)`, 1)
	if err != nil {
		if codegraph.IsGraphMissingError(err) {
			slog.Debug("archgraph: graph absent for package count", "graph", graph)
		} else {
			slog.Warn("archgraph: package count query failed", "graph", graph, "err", err)
		}
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(rows[0][0])
	return n
}

func queryCommunityCount(ctx context.Context, store *codegraph.Store, graph string) int {
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (s:Symbol) WHERE s.community IS NOT NULL RETURN count(DISTINCT s.community)`, 1)
	if err != nil {
		if codegraph.IsGraphMissingError(err) {
			slog.Debug("archgraph: graph absent for community count", "graph", graph)
		} else {
			slog.Warn("archgraph: community count query failed", "graph", graph, "err", err)
		}
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
	//
	// Group by p2.PATH (full import path), NOT p2.name (filepath.Base). Distinct
	// packages can share a base name — stdlib `embed` vs go-kit `.../embed`, or every
	// `.../v2` module collapsing to "v2". Grouping by name would SUM their importer
	// counts into one meaningless inflated entry (measured: "embed"=7 was 3+4 of two
	// unrelated packages, falsely crossing the god-package threshold). Since #185
	// unified local package nodes, p2.path is now a stable per-package identity.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p1:Package)-[:CONTAINS]->(f:File)-[:IMPORTS]->(p2:Package)
		 WHERE p1.path <> p2.path
		 RETURN p2.path, count(DISTINCT p1) AS importers
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
	// f.path is returned so we can exclude test-file imports (see cyclesFromRows).
	//
	// Identity is p.path (full import path), NOT p.name (filepath.Base): two distinct
	// packages sharing a base name must not be conflated into a phantom cycle. This is
	// only correct because #185 unified local package nodes — before it, a local
	// import resolved to a separate import-path node whose path format (full module
	// path) did not match the container's relative dir, so path-keying would have
	// failed to join the two ends of a cycle. Post-unify, local imports land on the
	// container node and both ends share the relative-dir path format.
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH (p1:Package)-[:CONTAINS]->(f:File)-[:IMPORTS]->(p2:Package)
		 WHERE p1.path <> p2.path
		 RETURN DISTINCT p1.path, p2.path, f.path`, 2)
	if err != nil {
		slog.Debug("archgraph: package imports query failed", "err", err)
		return nil
	}
	return cyclesFromRows(rows)
}

// cyclesFromRows builds a package import adjacency from (p1, p2, file_path) rows
// and returns the 2-cycles (A→B and B→A), deduplicated.
//
// Imports originating from a *_test.go file are SKIPPED. Go compiles an external
// test package (`package foo_test`) as a separate unit linked after both foo and
// its dependencies, so a foo_test.go file importing a package that imports foo is
// NOT a real import cycle. But the graph attributes files to their package by
// directory (goutil.PackageDir), folding foo_test.go into the `foo` node — which
// would manufacture a phantom foo→X edge. Internal white-box test files
// (`package foo`) importing X-that-imports-foo cannot exist (the Go compiler
// rejects that real cycle), so dropping ALL *_test.go imports loses no genuine
// cycle while removing the phantom ones.
// importRowCols is the column count of an ExecCypher row: p1.name, p2.name, f.path.
const importRowCols = 3

func cyclesFromRows(rows [][]string) []CircularDep {
	return find2Cycles(buildImportAdjacency(rows))
}

// buildImportAdjacency turns (p1, p2, file_path) rows into an A→B import map,
// skipping imports that originate from a *_test.go file (see cyclesFromRows).
func buildImportAdjacency(rows [][]string) map[string]map[string]bool {
	imports := make(map[string]map[string]bool)
	for _, row := range rows {
		if len(row) < importRowCols {
			continue
		}
		path := strings.Trim(row[2], `"`)
		if strings.HasSuffix(path, "_test.go") {
			continue // test-file import: never a real package-level cycle
		}
		a := strings.Trim(row[0], `"`)
		b := strings.Trim(row[1], `"`)
		if imports[a] == nil {
			imports[a] = make(map[string]bool)
		}
		imports[a][b] = true
	}
	return imports
}

// find2Cycles returns the mutual A→B / B→A package pairs, deduplicated by
// lexicographic ordering of the pair.
func find2Cycles(imports map[string]map[string]bool) []CircularDep {
	seen := make(map[string]bool)
	var result []CircularDep
	for a, deps := range imports {
		for b := range deps {
			if imports[b] == nil || !imports[b][a] {
				continue
			}
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
	return result
}
