package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// isTestFile reports whether a file path looks like a test file.
func isTestFile(path string) bool {
	return langutil.IsTestFile(path)
}

// isExternalImport reports whether an import path refers to an external module
// (i.e. the first path segment contains a dot, which is not the case for stdlib).
func isExternalImport(importPath string) bool {
	first := importPath
	if idx := strings.IndexByte(importPath, '/'); idx >= 0 {
		first = importPath[:idx]
	}
	return strings.ContainsRune(first, '.')
}

// hasTestAttribute reports whether a symbol has a test-related attribute
// (e.g. Rust's #[test] or #[cfg(test)]).
func hasTestAttribute(sym *parser.Symbol) bool {
	for _, attr := range sym.Attributes {
		if strings.Contains(attr, "test") {
			return true
		}
	}
	return false
}

// collectTestFilePaths returns the set of unique test-file paths found in snap.Symbols.
// Detects test files by filename pattern (Go, Python, JS/TS) and by symbol attributes
// (Rust's #[test] and #[cfg(test)] inline test modules).
func collectTestFilePaths(snap *RepoSnapshot) map[string]struct{} {
	seen := make(map[string]struct{})
	for _, sym := range snap.Symbols {
		if sym.File == "" {
			continue
		}
		if _, ok := seen[sym.File]; ok {
			continue
		}
		if isTestFile(sym.File) || hasTestAttribute(sym) {
			seen[sym.File] = struct{}{}
		}
	}
	return seen
}

// countExternalDeps returns the number of external (non-stdlib) import paths.
func countExternalDeps(imports []string) int {
	count := 0
	for _, imp := range imports {
		if isExternalImport(imp) {
			count++
		}
	}
	return count
}

// largeFileThreshold is the line count above which a file is considered large.
const largeFileThreshold = 250

// computeLargeFileRatio returns the fraction of files exceeding largeFileThreshold lines.
func computeLargeFileRatio(files []SnapshotFile) float64 {
	if len(files) == 0 {
		return 0
	}
	large := 0
	for _, f := range files {
		if f.Lines > largeFileThreshold {
			large++
		}
	}
	return float64(large) / float64(len(files))
}

// computeDuplicationRatio returns the fraction of functions with duplicate BodyHash.
// Uses the existing xxhash-based BodyHash on Symbol.
func computeDuplicationRatio(symbols []*parser.Symbol) float64 {
	// Count only functions/methods with non-zero BodyHash in the denominator.
	hashCounts := make(map[uint64]int)
	funcCount := 0
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if sym.BodyHash == 0 {
			continue
		}
		funcCount++
		hashCounts[sym.BodyHash]++
	}
	if funcCount == 0 {
		return 0
	}

	// Count how many functions share a hash with at least one other.
	duplicated := 0
	for _, count := range hashCounts {
		if count > 1 {
			duplicated += count
		}
	}
	return float64(duplicated) / float64(funcCount)
}
