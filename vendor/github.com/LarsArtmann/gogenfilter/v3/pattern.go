package gogenfilter

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// MatchPattern checks if a path matches a pattern.
// Supports * (matches non-separator characters) and ** (matches any path segments).
// Patterns without path separators match against the filename only.
func MatchPattern(path, pattern string) bool {
	if !strings.ContainsAny(pattern, "/\\") {
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil {
			return false
		}

		return matched
	}

	normalizedPath := filepath.ToSlash(path)
	normalizedPattern := filepath.ToSlash(pattern)

	// For absolute paths with relative patterns, prepend **/ to match at any depth.
	if strings.HasPrefix(normalizedPath, "/") &&
		!strings.HasPrefix(normalizedPattern, "/") &&
		!strings.HasPrefix(normalizedPattern, "**") {
		normalizedPattern = "**/" + normalizedPattern
	}

	matched, err := doublestar.Match(normalizedPattern, normalizedPath)
	if err != nil {
		return false
	}

	return matched
}
