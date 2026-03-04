package analyze

import "strings"

// addRustImports handles Rust use declarations for the import graph.
func addRustImports(graph importGraph, pkg string, imports []string, includeStdlib bool) {
	if _, ok := graph[pkg]; !ok {
		graph[pkg] = make(map[string]struct{})
	}
	for _, imp := range imports {
		if imp == "" {
			continue
		}
		imp = strings.Trim(imp, "\"'{}")
		if !includeStdlib && isRustStdlib(imp) {
			continue
		}
		if strings.HasPrefix(imp, "self::") {
			continue
		}
		graph[pkg][imp] = struct{}{}
	}
}

// isRustStdlib returns true for Rust standard library imports.
func isRustStdlib(imp string) bool {
	root, _, _ := strings.Cut(imp, "::")
	switch root {
	case "std", "core", "alloc":
		return true
	}
	return false
}
