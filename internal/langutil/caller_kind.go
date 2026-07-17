package langutil

import "strings"

// Canonical caller-kind values used by understand, call_trace, and related tools.
const (
	CallerKindProduction = "production"
	CallerKindTest       = "test"
	CallerKindExample    = "example"
	CallerKindBenchmark  = "benchmark"
	CallerKindUnresolved = "unresolved"
)

// CallerKind classifies a symbol as production/test/example/benchmark from its
// name and file path. It is used by understand and call_trace to annotate
// callers so blast-radius answers can distinguish production call sites from
// tests.
//
// The file is the gate: any caller in a non-test file is production, regardless
// of its name. Only callers inside a recognised test file are sub-classified by
// name (Example -> example, Benchmark -> benchmark, everything else -> test).
// This is intentionally multi-language: IsTestFile covers Go _test.go, Python
// test_/_test.py, JS/TS .test./.spec., test dirs, etc.
func CallerKind(name, relPath string) string {
	if !IsTestFile(relPath) {
		return CallerKindProduction
	}
	if strings.HasPrefix(name, "Example") {
		return CallerKindExample
	}
	if strings.HasPrefix(name, "Benchmark") {
		return CallerKindBenchmark
	}
	return CallerKindTest
}
