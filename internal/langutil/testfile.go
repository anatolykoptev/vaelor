// Package langutil provides shared language-aware helpers used across go-code packages.
package langutil

import (
	"path/filepath"
	"strings"
)

// testSuffixes covers Go, Python, Rust, JS/TS test file patterns.
var testSuffixes = []string{
	"_test.go", "_test.py", "_test.rs",
	".test.ts", ".test.js", ".test.tsx",
	".spec.ts", ".spec.js", ".spec.tsx",
}

// testExactNames covers exact file names that are test files.
var testExactNames = []string{"tests.rs"}

// testPrefixes covers Python test file naming.
var testPrefixes = []string{"test_"}

// testDirs covers common test directories.
var testDirs = []string{"/test/", "/tests/"}

// IsTestFile returns true if the path looks like a test file
// based on suffix, prefix, or containing directory.
func IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	base := filepath.Base(lower)

	for _, suf := range testSuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	for _, exact := range testExactNames {
		if base == exact {
			return true
		}
	}
	for _, pre := range testPrefixes {
		if strings.HasPrefix(base, pre) {
			return true
		}
	}
	for _, dir := range testDirs {
		if strings.Contains(lower, dir) {
			return true
		}
	}
	return false
}

// RelPath returns the relative path from root to abs.
// Falls back to the absolute path on error.
func RelPath(abs, root string) string {
	if root == "" {
		return abs
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return strings.TrimPrefix(abs, root+"/")
	}
	return rel
}
