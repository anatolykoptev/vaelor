package embeddings

import (
	"testing"
	"time"
)

// helper: build a SearchResult with a given symbol name and updated_at.
func resultAt(name string, updatedAt time.Time) SearchResult {
	return SearchResult{
		SymbolName: name,
		FilePath:   name + ".go",
		UpdatedAt:  updatedAt,
	}
}

// symbolNames extracts SymbolName from a result slice — used for order assertions.
func symbolNames(results []SearchResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.SymbolName
	}
	return names
}

// TestStaleDemote_StaleRowDemotedBelowFresh is the primary correctness test.
// A result whose updated_at is older than (generation - epsilon) must appear
// AFTER all fresh results.
//
// Red-on-revert: removing the partition from ApplyStaleDemote leaves the stale
// row at its distance-rank position (first), and the fresh rows after → order
// is ["stale", "freshA", "freshB"] → assertion on names[0]=="freshA" FAILS.
func TestStaleDemote_StaleRowDemotedBelowFresh(t *testing.T) {
	now := time.Now().UTC()
	generation := now

	// "stale" has updated_at = 2 hours ago (well below generation-epsilon).
	// "freshA" and "freshB" have updated_at = now (above cutoff).
	results := []SearchResult{
		resultAt("stale", now.Add(-2*time.Hour)),
		resultAt("freshA", now),
		resultAt("freshB", now.Add(-10*time.Second)),
	}

	out := ApplyStaleDemote(results, generation, true)

	if len(out) != 3 {
		t.Fatalf("expected 3 results, got %d", len(out))
	}
	names := symbolNames(out)
	// Fresh results must occupy positions 0 and 1.
	if names[0] != "freshA" {
		t.Errorf("position 0: got %q, want %q (stale row must be demoted)", names[0], "freshA")
	}
	if names[1] != "freshB" {
		t.Errorf("position 1: got %q, want %q (fresh order must be preserved)", names[1], "freshB")
	}
	// Stale row must be last.
	if names[2] != "stale" {
		t.Errorf("position 2: got %q, want %q (stale row must be last)", names[2], "stale")
	}
}

// TestStaleDemote_ByteIdenticalWhenAllFresh verifies that when all results have
// updated_at >= (generation - epsilon), the function returns the SAME slice
// (no copy, no reordering).
//
// Red-on-revert: if ApplyStaleDemote always allocates a new slice regardless,
// the &out[0] != &results[0] pointer check fires → FAIL.
func TestStaleDemote_ByteIdenticalWhenAllFresh(t *testing.T) {
	now := time.Now().UTC()
	generation := now

	results := []SearchResult{
		resultAt("alpha", now),
		resultAt("beta", now.Add(-30*time.Second)),
		resultAt("gamma", now.Add(-time.Second)),
	}

	out := ApplyStaleDemote(results, generation, true)

	if len(out) != len(results) {
		t.Fatalf("expected %d results, got %d", len(results), len(out))
	}
	// Same backing array — no copy made.
	if len(out) > 0 && &out[0] != &results[0] {
		t.Error("byte-identical contract violated: ApplyStaleDemote returned a new slice when all rows are fresh")
	}
	// Order unchanged.
	for i, r := range out {
		if r.SymbolName != results[i].SymbolName {
			t.Errorf("position %d: got %q, want %q", i, r.SymbolName, results[i].SymbolName)
		}
	}
}

// TestStaleDemote_DisabledByFlag verifies STALE_DEMOTE=off semantics:
// passing enabled=false must return the slice unchanged regardless of updated_at.
//
// Red-on-revert: if enabled=false is ignored (always demotes), the stale row
// stays first in original order → names[0]=="stale" → the position-0 assertion
// checking for the original order passes, but removing the enabled guard means
// the partition runs → names[0]!="stale" → FAIL.
func TestStaleDemote_DisabledByFlag(t *testing.T) {
	now := time.Now().UTC()
	generation := now

	results := []SearchResult{
		resultAt("stale", now.Add(-24*time.Hour)), // would be demoted if enabled
		resultAt("fresh", now),
	}

	out := ApplyStaleDemote(results, generation, false) // disabled

	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	// With flag off, original order must be preserved (same slice header).
	if &out[0] != &results[0] {
		t.Error("disabled flag: expected same slice header (no-op), got copy")
	}
	if out[0].SymbolName != "stale" {
		t.Errorf("disabled flag: position 0 = %q, want %q (original order)", out[0].SymbolName, "stale")
	}
}

// TestStaleDemote_CounterBumpedPerDemotion verifies the counter increments for
// each demoted row. Uses prometheus's test registry via a read-before-after.
//
// Red-on-revert: removing staleDemotedTotal.Add(1) from the partition loop
// leaves count unchanged → before == after → assertion FAILS.
func TestStaleDemote_CounterBumpedPerDemotion(t *testing.T) {
	now := time.Now().UTC()
	generation := now

	before := readCounter(t, "gocode_semantic_stale_demoted_total")

	results := []SearchResult{
		resultAt("stale1", now.Add(-2*time.Hour)),
		resultAt("stale2", now.Add(-3*time.Hour)),
		resultAt("fresh", now),
	}

	_ = ApplyStaleDemote(results, generation, true)

	after := readCounter(t, "gocode_semantic_stale_demoted_total")
	if after-before != 2 {
		t.Errorf("expected counter to increment by 2 (one per demoted row), got delta=%.0f", after-before)
	}
}

// TestStaleDemote_ZeroGenerationNoOp verifies that a zero generation time
// (store unavailable / repo never indexed) is a no-op — same slice returned.
//
// Red-on-revert: if zero-generation check is removed, cutoff becomes zero
// time, and rows with any updatedAt after zero will look "fresh" → no
// demotion → the function returns the original slice — which is actually the
// same outcome. So instead we check: the function must NOT allocate a new
// slice, i.e., same pointer.
func TestStaleDemote_ZeroGenerationNoOp(t *testing.T) {
	now := time.Now().UTC()

	results := []SearchResult{
		resultAt("a", now.Add(-24*time.Hour)),
		resultAt("b", now),
	}

	out := ApplyStaleDemote(results, time.Time{}, true) // zero generation

	if &out[0] != &results[0] {
		t.Error("zero generation: expected same slice header (no-op), got copy")
	}
}
