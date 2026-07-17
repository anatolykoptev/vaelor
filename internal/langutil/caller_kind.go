package langutil

import "strings"

// CallerKind classifies a symbol as production/test/example/benchmark from its
// name and file path. It is used by understand and call_trace to annotate
// callers so blast-radius answers can distinguish production call sites from
// tests.
//
// Rules (Go primary, extended to common per-language conventions):
//   - name starts with "Example" -> "example"
//   - name starts with "Benchmark" -> "benchmark"
//   - name starts with "Test", "Fuzz", or "test_" -> "test"
//   - the file is a recognised test file -> "test"
//   - otherwise -> "production"
func CallerKind(name, relPath string) string {
	switch {
	case strings.HasPrefix(name, "Example"):
		return "example"
	case strings.HasPrefix(name, "Benchmark"):
		return "benchmark"
	case strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "test_"):
		return "test"
	case IsTestFile(relPath):
		return "test"
	default:
		return "production"
	}
}
