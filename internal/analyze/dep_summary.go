package analyze

import (
	"fmt"
	"sort"
	"strings"
)

// renderSummary produces a compact text overview of the dependency graph:
// package/edge counts, top fan-in/fan-out, cycles, and leaf packages.
func renderSummary(graph importGraph) string {
	var sb strings.Builder

	pkgCount := len(graph)
	edgeCount := countEdges(graph)
	fmt.Fprintf(&sb, "Packages: %d | Edges: %d\n\n", pkgCount, edgeCount)

	fanIn := computeFanIn(graph)
	writeTopN(&sb, "Top fan-in (most imported)", fanIn, 5)  //nolint:mnd
	writeTopN(&sb, "Top fan-out (most dependencies)", fanOut(graph), 5) //nolint:mnd

	cycles := detectCycles(graph)
	if len(cycles) > 0 {
		fmt.Fprintf(&sb, "Cycles detected: %d\n", len(cycles))
		const maxCycles = 5
		for i, c := range cycles {
			if i >= maxCycles {
				fmt.Fprintf(&sb, "  ... and %d more\n", len(cycles)-maxCycles)
				break
			}
			fmt.Fprintf(&sb, "  %s\n", strings.Join(c, " → "))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Cycles: none\n\n")
	}

	writeLeaves(&sb, graph, fanIn)

	return sb.String()
}

// countEdges returns the total number of edges in the graph.
func countEdges(graph importGraph) int {
	n := 0
	for _, deps := range graph {
		n += len(deps)
	}
	return n
}

// pkgCount is a package name with an associated count for sorting.
type pkgCount struct {
	Name  string
	Count int
}

// computeFanIn counts how many packages import each dependency.
func computeFanIn(graph importGraph) []pkgCount {
	counts := make(map[string]int)
	for _, deps := range graph {
		for dep := range deps {
			counts[dep]++
		}
	}
	return sortedPkgCounts(counts)
}

// fanOut returns packages sorted by number of dependencies (descending).
func fanOut(graph importGraph) []pkgCount {
	counts := make(map[string]int, len(graph))
	for pkg, deps := range graph {
		counts[pkg] = len(deps)
	}
	return sortedPkgCounts(counts)
}

func sortedPkgCounts(counts map[string]int) []pkgCount {
	result := make([]pkgCount, 0, len(counts))
	for name, count := range counts {
		result = append(result, pkgCount{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// writeTopN writes the top N entries from a pkgCount slice.
func writeTopN(sb *strings.Builder, title string, items []pkgCount, n int) {
	fmt.Fprintf(sb, "%s:\n", title)
	limit := n
	if len(items) < limit {
		limit = len(items)
	}
	if limit == 0 {
		sb.WriteString("  (none)\n")
	}
	for i := range limit {
		fmt.Fprintf(sb, "  %s (%d)\n", items[i].Name, items[i].Count)
	}
	sb.WriteString("\n")
}

// detectCycles finds all minimal cycles in the import graph using DFS.
// Returns cycles as slices of package names forming the cycle path.
func detectCycles(graph importGraph) [][]string {
	var cycles [][]string
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var path []string

	var dfs func(node string)
	dfs = func(node string) {
		visited[node] = true
		inStack[node] = true
		path = append(path, node)

		for dep := range graph[node] {
			if !visited[dep] {
				dfs(dep)
			} else if inStack[dep] {
				// Found a cycle — extract it from path.
				start := -1
				for i, p := range path {
					if p == dep {
						start = i
						break
					}
				}
				if start >= 0 {
					cycle := make([]string, len(path)-start+1)
					copy(cycle, path[start:])
					cycle[len(cycle)-1] = dep // close the cycle
					cycles = append(cycles, cycle)
				}
			}
		}

		path = path[:len(path)-1]
		inStack[node] = false
	}

	for node := range graph {
		if !visited[node] {
			dfs(node)
		}
	}

	return cycles
}

// writeLeaves lists leaf packages: those with no deps (fan-out=0) or not imported (fan-in=0).
func writeLeaves(sb *strings.Builder, graph importGraph, fanIn []pkgCount) {
	importedSet := make(map[string]struct{})
	for _, fc := range fanIn {
		importedSet[fc.Name] = struct{}{}
	}

	var noOut, noIn []string
	for pkg, deps := range graph {
		if len(deps) == 0 {
			noOut = append(noOut, pkg)
		}
		if _, ok := importedSet[pkg]; !ok {
			noIn = append(noIn, pkg)
		}
	}
	sort.Strings(noOut)
	sort.Strings(noIn)

	if len(noOut) > 0 {
		fmt.Fprintf(sb, "Leaf packages (no dependencies): %s\n", strings.Join(noOut, ", "))
	}
	if len(noIn) > 0 {
		fmt.Fprintf(sb, "Root packages (not imported): %s\n", strings.Join(noIn, ", "))
	}
}
