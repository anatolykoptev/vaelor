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
	// Detect both Unix absolute paths (leading /) and Windows drive paths
	// (C:/... after ToSlash): a drive-colon path must not be treated as
	// relative, or depth-anchored patterns never match on Windows.
	if isAbsLike(normalizedPath) &&
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

// isAbsLike reports whether a slash-normalized path is absolute: a leading
// slash (Unix, and UNC after ToSlash) or a Windows drive prefix (C:/).
func isAbsLike(p string) bool {
	return strings.HasPrefix(p, "/") ||
		(len(p) >= 3 && p[1] == ':' && p[2] == '/' &&
			(p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z'))
}
