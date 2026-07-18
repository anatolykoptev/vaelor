package analyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/goutil"
)

// importGraph maps a package path to the set of packages it imports.
type importGraph map[string]map[string]struct{}

// buildImportGraph builds a package-level import graph from parse results.
func buildImportGraph(root string, results []fileParseResult, includeStdlib bool) importGraph {
	graph := make(importGraph)

	for _, pr := range results {
		if pr.result == nil || len(pr.result.Imports) == 0 {
			continue
		}
		pkg := goutil.PackageDir(root, pr.file.Path)
		if pr.result.Language == "rust" {
			addRustImports(graph, pkg, pr.result.Imports, includeStdlib)
		} else {
			addImports(graph, pkg, pr.result.Imports, includeStdlib)
		}
	}

	return graph
}

// addImports inserts all valid imports for a package into the graph.
func addImports(graph importGraph, pkg string, imports []string, includeStdlib bool) {
	if _, ok := graph[pkg]; !ok {
		graph[pkg] = make(map[string]struct{})
	}
	for _, imp := range imports {
		if imp == "" {
			continue
		}
		if !includeStdlib && goutil.IsStdlibImport(imp) {
			continue
		}
		if strings.HasSuffix(imp, "/"+pkg) {
			continue
		}
		graph[pkg][imp] = struct{}{}
	}
}

// filterGraph returns a subgraph reachable from focus within maxDepth hops.
// maxDepth <= 0 means no limit.
func filterGraph(graph importGraph, focus string, maxDepth int) importGraph {
	result := make(importGraph)
	visited := make(map[string]int)

	var walk func(node string, depth int)
	walk = func(node string, depth int) {
		if _, seen := visited[node]; seen {
			return
		}
		if maxDepth > 0 && depth > maxDepth {
			return
		}
		visited[node] = depth

		deps, ok := graph[node]
		if !ok {
			return
		}
		result[node] = deps
		for dep := range deps {
			walk(dep, depth+1)
		}
	}

	for pkg := range graph {
		if strings.Contains(pkg, focus) {
			walk(pkg, 0)
		}
	}

	return result
}

// renderGraph formats the import graph in the requested output format.
func renderGraph(graph importGraph, format string) (string, error) {
	switch format {
	case "mermaid", "":
		return renderMermaid(graph), nil
	case "dot":
		return renderDot(graph), nil
	case "json":
		return renderJSON(graph)
	case "summary":
		return renderSummary(graph), nil
	default:
		return "", fmt.Errorf("unsupported format %q: use mermaid, dot, json, or summary", format)
	}
}

// renderMermaid renders the graph as a Mermaid diagram.
func renderMermaid(graph importGraph) string {
	var sb strings.Builder
	sb.WriteString("graph TD\n")

	pkgs := sortedKeys(graph)
	for _, pkg := range pkgs {
		deps := graph[pkg]
		safeFrom := mermaidID(pkg)
		if len(deps) == 0 {
			fmt.Fprintf(&sb, "    %s\n", safeFrom)
			continue
		}
		sortedDeps := goutil.SortedSetKeys(deps)
		for _, dep := range sortedDeps {
			safeDep := mermaidID(dep)
			fmt.Fprintf(&sb, "    %s --> %s\n", safeFrom, safeDep)
		}
	}
	return sb.String()
}

// renderDot renders the graph in Graphviz DOT format.
func renderDot(graph importGraph) string {
	var sb strings.Builder
	sb.WriteString("digraph deps {\n")
	sb.WriteString("    rankdir=LR;\n")

	pkgs := sortedKeys(graph)
	for _, pkg := range pkgs {
		deps := graph[pkg]
		sortedDeps := goutil.SortedSetKeys(deps)
		for _, dep := range sortedDeps {
			fmt.Fprintf(&sb, "    %q -> %q;\n", pkg, dep)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// renderJSON renders the graph as a JSON adjacency list.
func renderJSON(graph importGraph) (string, error) {
	out := make(map[string][]string, len(graph))
	for pkg, deps := range graph {
		out[pkg] = goutil.SortedSetKeys(deps)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshal graph: %w", err)
	}
	return string(b), nil
}

// mermaidID converts an arbitrary string to a valid Mermaid node ID.
func mermaidID(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

// sortedKeys returns sorted keys of an importGraph map.
func sortedKeys(m importGraph) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
