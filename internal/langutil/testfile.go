// Package langutil provides shared language-aware helpers used across go-code packages.
package langutil

import (
	"path/filepath"
	"strings"
)

// testSuffixes covers Go, Python, Rust test file patterns.
var testSuffixes = []string{
	"_test.go", "_test.py", "_test.rs",
}

// testInfixes covers JS/TS/Svelte/Astro infix-based test file patterns.
// TestStem uses these to extract the stem (e.g. "Button" from "Button.test.ts").
var testInfixes = []string{".test.", ".spec."}

// testExactNames covers exact file names that are test files.
var testExactNames = []string{"tests.rs"}

// testPrefixes covers Python test file naming.
var testPrefixes = []string{"test_"}

// testDirs covers common test directories.
var testDirs = []string{"/test/", "/tests/"}

// TestStem extracts the logical stem from an infix-based test file path
// (e.g. "Button.test.ts" → "Button", true; "Modal.spec.svelte" → "Modal", true).
// It uses the first occurrence of ".test." or ".spec." in the file base-name.
// Returns ("", false) if the path does not contain a recognised infix or if
// the stem before the infix is empty (e.g. ".test.ts").
func TestStem(path string) (stem string, ok bool) {
	base := filepath.Base(path)
	for _, infix := range testInfixes {
		if idx := strings.Index(base, infix); idx > 0 {
			return base[:idx], true
		}
	}
	return "", false
}

// IsTestFile returns true if the path looks like a test file
// based on suffix, prefix, infix, or containing directory.
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
	if _, ok := TestStem(lower); ok {
		return true
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
