package explore

import (
	"sort"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/goutil"
	"github.com/anatolykoptev/vaelor/internal/ingest"
)

// DepHighlights is a compact dependency summary for explore results.
type DepHighlights struct {
	ExternalDeps int        `json:"external_deps"`
	TopFanIn     []DepEntry `json:"top_fan_in,omitempty"`
	TopFanOut    []DepEntry `json:"top_fan_out,omitempty"`
	HasCycles    bool       `json:"has_cycles"`
}

// DepEntry is a package with an associated count.
type DepEntry struct {
	Package string `json:"package"`
	Count   int    `json:"count"`
}

// maxDepEntries is the number of top fan-in/fan-out entries to include.
const maxDepEntries = 3

// isStdlibImport checks for stdlib imports across languages.
func isStdlibImport(imp string) bool {
	if strings.Contains(imp, "::") {
		root, _, _ := strings.Cut(imp, "::")
		return root == "std" || root == "core" || root == "alloc"
	}
	return goutil.IsStdlibImport(imp)
}

// buildDepHighlights computes a lightweight dependency overview from parse results.
func buildDepHighlights(files []*ingest.File, imports map[string][]string, root string) *DepHighlights {
	// Build package-level import graph: pkg → set of imported paths.
	type depSet = map[string]struct{}
	graph := make(map[string]depSet)
	allExternal := make(map[string]struct{})

	for path, fileImports := range imports {
		pkg := goutil.PackageDir(root, path)
		if _, ok := graph[pkg]; !ok {
			graph[pkg] = make(depSet)
		}
		for _, imp := range fileImports {
			if imp == "" || isStdlibImport(imp) {
				continue
			}
			graph[pkg][imp] = struct{}{}
			allExternal[imp] = struct{}{}
		}
	}

	// Fan-in: how many packages import each dep.
	fanInCounts := make(map[string]int)
	for _, deps := range graph {
		for dep := range deps {
			fanInCounts[dep]++
		}
	}

	// Fan-out: how many deps each package has.
	fanOutCounts := make(map[string]int, len(graph))
	for pkg, deps := range graph {
		fanOutCounts[pkg] = len(deps)
	}

	return &DepHighlights{
		ExternalDeps: len(allExternal),
		TopFanIn:     topN(fanInCounts, maxDepEntries),
		TopFanOut:    topN(fanOutCounts, maxDepEntries),
		HasCycles:    hasCycle(graph),
	}
}

// topN returns the top N entries from a count map, sorted descending.
func topN(counts map[string]int, n int) []DepEntry {
	entries := make([]DepEntry, 0, len(counts))
	for name, count := range counts {
		entries = append(entries, DepEntry{Package: name, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Package < entries[j].Package
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}

// hasCycle performs a DFS to detect any cycle in the graph.
func hasCycle(graph map[string]map[string]struct{}) bool {
	white := make(map[string]bool, len(graph)) // unvisited
	gray := make(map[string]bool)              // in current path

	for node := range graph {
		white[node] = true
	}

	var dfs func(string) bool
	dfs = func(node string) bool {
		white[node] = false
		gray[node] = true

		for dep := range graph[node] {
			if gray[dep] {
				return true
			}
			if white[dep] {
				if dfs(dep) {
					return true
				}
			}
		}
		gray[node] = false
		return false
	}

	for node := range graph {
		if white[node] {
			if dfs(node) {
				return true
			}
		}
	}
	return false
}
