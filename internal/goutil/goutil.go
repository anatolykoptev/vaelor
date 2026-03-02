// Package goutil provides shared utility functions used across
// multiple internal packages (analyze, explore, compare, codegraph).
package goutil

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
)

// IsStdlibImport reports whether an import path looks like a Go stdlib package.
// Stdlib paths have no dots in the first segment (e.g. "fmt", "net/http").
func IsStdlibImport(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}

// PackageDir returns the directory portion of filePath relative to root.
// If the relative dir is ".", it falls back to filepath.Base(root).
// This matches the convention used for package-level import graphs.
func PackageDir(root, filePath string) string {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		rel = filePath
	}
	dir := filepath.Dir(rel)
	if dir == "." {
		dir = filepath.Base(root)
	}
	return dir
}

// SortedSetKeys returns sorted keys of a set (map[string]struct{}).
func SortedSetKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CountLines returns the number of lines in source.
// A trailing newline does not count as an extra line.
// Returns 0 for empty input.
func CountLines(source []byte) int {
	if len(source) == 0 {
		return 0
	}
	n := bytes.Count(source, []byte("\n"))
	if source[len(source)-1] != '\n' {
		n++
	}
	return n
}
