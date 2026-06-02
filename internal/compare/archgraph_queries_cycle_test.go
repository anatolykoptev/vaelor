package compare

import "testing"

// TestCyclesFromRows_PhantomTestImportIsNotACycle is the regression guard for the
// false-positive circular dependency that code_health reported for
// callgraph↔goanalysis: goanalysis/convert_test.go (package goanalysis_test) imports
// callgraph, while callgraph imports goanalysis. Go compiles the external test
// package separately, so there is NO real cycle — but the graph attributes the test
// file to the `goanalysis` package node and manufactures a phantom goanalysis→callgraph
// edge. cyclesFromRows must skip *_test.go imports so this is NOT reported.
//
// Falsification (red-on-revert): remove the `_test.go` skip in cyclesFromRows and this
// test fails (the phantom cycle reappears).
func TestCyclesFromRows_PhantomTestImportIsNotACycle(t *testing.T) {
	rows := [][]string{
		{"callgraph", "goanalysis", "internal/callgraph/convert.go"},       // real prod edge
		{"goanalysis", "callgraph", "internal/goanalysis/convert_test.go"}, // phantom: test file only
	}
	got := cyclesFromRows(rows)
	if len(got) != 0 {
		t.Fatalf("phantom test-only back-edge reported as cycle: %+v (want none)", got)
	}
}

// TestCyclesFromRows_RealCycleDetected confirms a genuine two-package cycle backed by
// production files is still reported (the skip must not over-filter).
func TestCyclesFromRows_RealCycleDetected(t *testing.T) {
	rows := [][]string{
		{"a", "b", "internal/a/a.go"},
		{"b", "a", "internal/b/b.go"},
	}
	got := cyclesFromRows(rows)
	if len(got) != 1 {
		t.Fatalf("real cycle: got %d cycles %+v, want 1", len(got), got)
	}
	// Canonical order: lexicographically smaller package first.
	if got[0].PackageA != "a" || got[0].PackageB != "b" {
		t.Errorf("cycle = {%s,%s}, want {a,b}", got[0].PackageA, got[0].PackageB)
	}
}

// TestCyclesFromRows_ProdAndTestEdgeStillCycle confirms that when the back-edge is
// supported by BOTH a production file and a test file, the cycle is still real
// (the production edge survives the test-file skip).
func TestCyclesFromRows_ProdAndTestEdgeStillCycle(t *testing.T) {
	rows := [][]string{
		{"a", "b", "internal/a/a.go"},
		{"b", "a", "internal/b/b.go"},      // real back-edge
		{"b", "a", "internal/b/b_test.go"}, // also a test edge — must not change the verdict
	}
	got := cyclesFromRows(rows)
	if len(got) != 1 {
		t.Fatalf("prod+test back-edge: got %d cycles %+v, want 1", len(got), got)
	}
}

// TestCyclesFromRows_NoCycle confirms a one-directional import is not a cycle.
func TestCyclesFromRows_NoCycle(t *testing.T) {
	rows := [][]string{
		{"a", "b", "internal/a/a.go"},
	}
	if got := cyclesFromRows(rows); len(got) != 0 {
		t.Fatalf("one-way import reported as cycle: %+v", got)
	}
}

// TestCyclesFromRows_QuotedValues confirms AGE-style double-quoted row values are
// trimmed before comparison (the live ExecCypher rows arrive quoted).
func TestCyclesFromRows_QuotedValues(t *testing.T) {
	rows := [][]string{
		{`"a"`, `"b"`, `"internal/a/a.go"`},
		{`"b"`, `"a"`, `"internal/b/b.go"`},
	}
	if got := cyclesFromRows(rows); len(got) != 1 {
		t.Fatalf("quoted values: got %d cycles %+v, want 1", len(got), got)
	}
}
