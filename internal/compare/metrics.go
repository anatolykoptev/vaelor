package compare

import (
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// isTestFile reports whether a file path looks like a test file.
// Recognised patterns: *_test.go, *_test.py, *.test.ts, *.test.js,
// *.spec.ts, *.spec.js, and paths that contain a /test/ or /tests/ segment.
func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	testSuffixes := []string{
		"_test.go", "_test.py",
		".test.ts", ".test.js",
		".spec.ts", ".spec.js",
	}
	for _, suf := range testSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/")
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

// isExported reports whether a symbol name is exported (starts with an uppercase letter).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return unicode.IsUpper(r)
}

// funcLines returns the line count for a function/method symbol.
func funcLines(sym *parser.Symbol) int {
	if sym.EndLine >= sym.StartLine {
		return int(sym.EndLine-sym.StartLine) + 1
	}
	return 1
}

// ComputeMetrics derives aggregate quality metrics from a RepoSnapshot.
//
// The snapshot must already have Symbols and Imports populated; FileCount and
// TotalLines are copied verbatim. Test-file detection is based on Symbol.File
// paths (unique files seen across all symbols).
func ComputeMetrics(snap *RepoSnapshot) RepoMetrics {
	// --- function/method lines ---
	totalFuncLines := 0
	funcCount := 0
	maxFuncLines := 0

	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		lines := funcLines(sym)
		totalFuncLines += lines
		funcCount++
		if lines > maxFuncLines {
			maxFuncLines = lines
		}
	}

	var avgFuncLines float64
	if funcCount > 0 {
		avgFuncLines = float64(totalFuncLines) / float64(funcCount)
	}

	// --- test-file ratio ---
	// Collect unique file paths from symbols to detect test files.
	testFilePaths := collectTestFilePaths(snap)

	var testFileRatio float64
	if snap.FileCount > 0 {
		testFileRatio = float64(len(testFilePaths)) / float64(snap.FileCount)
	}

	// --- doc-comment ratio ---
	docRatio := computeDocRatio(snap.Symbols)

	// --- external deps ---
	externalDeps := countExternalDeps(snap.Imports)

	// --- error handling ratio ---
	errorHandlingRatio := computeErrorHandlingRatio(snap.Symbols)

	// --- interface count ---
	interfaceCount := countInterfaces(snap.Symbols)

	return RepoMetrics{
		Files:              snap.FileCount,
		TotalLines:         snap.TotalLines,
		AvgFuncLines:       avgFuncLines,
		MaxFuncLines:       maxFuncLines,
		TestRatio:          testFileRatio,
		DocRatio:           docRatio,
		ErrorHandlingRatio: errorHandlingRatio,
		Interfaces:         interfaceCount,
		ExternalDeps:       externalDeps,
	}
}

// collectTestFilePaths returns the set of unique test-file paths found in snap.Symbols.
func collectTestFilePaths(snap *RepoSnapshot) map[string]struct{} {
	seen := make(map[string]struct{})
	for _, sym := range snap.Symbols {
		if sym.File == "" {
			continue
		}
		if _, ok := seen[sym.File]; ok {
			continue
		}
		if isTestFile(sym.File) {
			seen[sym.File] = struct{}{}
		}
	}
	return seen
}

// computeDocRatio returns the fraction of exported symbols that have a doc comment.
func computeDocRatio(symbols []*parser.Symbol) float64 {
	exportedTotal := 0
	exportedWithDoc := 0
	for _, sym := range symbols {
		if !isExported(sym.Name) {
			continue
		}
		exportedTotal++
		if sym.DocComment != "" {
			exportedWithDoc++
		}
	}
	if exportedTotal == 0 {
		return 0
	}
	return float64(exportedWithDoc) / float64(exportedTotal)
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

// errorHandlingPatterns are substrings that reliably indicate error handling in function bodies.
var errorHandlingPatterns = []string{
	"if err ",
	"if err!",
	"!= nil",
	"err :=",
	"err =",
	"return err",
	"return fmt.Errorf",
	"errors.New",
	"errors.Is(",
	"errors.As(",
	"errors.Join(",
	".Error()",
	"except ",  // Python
	"catch (",  // Java/TS
	"catch(",
	"rescue ",  // Ruby
}

// computeErrorHandlingRatio returns the fraction of functions/methods whose body
// contains reliable error-handling patterns (not just the substring "err").
func computeErrorHandlingRatio(symbols []*parser.Symbol) float64 {
	funcCount := 0
	withErrorHandling := 0
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		funcCount++
		if hasErrorHandling(sym.Body) {
			withErrorHandling++
		}
	}
	if funcCount == 0 {
		return 0
	}
	return float64(withErrorHandling) / float64(funcCount)
}

// hasErrorHandling checks whether a function body contains reliable error handling patterns.
func hasErrorHandling(body string) bool {
	for _, pattern := range errorHandlingPatterns {
		if strings.Contains(body, pattern) {
			return true
		}
	}
	return false
}

// countInterfaces returns the number of interface symbols.
func countInterfaces(symbols []*parser.Symbol) int {
	count := 0
	for _, sym := range symbols {
		if sym.Kind == parser.KindInterface {
			count++
		}
	}
	return count
}
