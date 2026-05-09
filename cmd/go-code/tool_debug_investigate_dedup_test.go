// cmd/go-code/tool_debug_investigate_dedup_test.go
// Tests for dedupByRepoSymbol and the dedup behaviour in retrieveHistoricalIncidents.
package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/learnings"
)

// TestDedupByRepoSymbol_SameSymbol verifies that duplicate (Repo, Symbol) pairs
// are collapsed to the first occurrence (highest similarity score wins).
func TestDedupByRepoSymbol_SameSymbol(t *testing.T) {
	records := []learnings.Record{
		{Repo: "svc", Symbol: "x", Note: "first"},
		{Repo: "svc", Symbol: "x", Note: "second"},
		{Repo: "svc", Symbol: "y", Note: "third"},
	}
	got := dedupByRepoSymbol(records)
	if len(got) != 2 {
		t.Fatalf("expected 2 records after dedup, got %d: %v", len(got), got)
	}
	if got[0].Note != "first" {
		t.Errorf("first occurrence should win: got Note=%q", got[0].Note)
	}
	if got[1].Symbol != "y" {
		t.Errorf("second record should be y: got Symbol=%q", got[1].Symbol)
	}
}

// TestDedupByRepoSymbol_DifferentSymbols verifies that records with distinct
// (Repo, Symbol) pairs are all preserved — no false dedup.
func TestDedupByRepoSymbol_DifferentSymbols(t *testing.T) {
	records := []learnings.Record{
		{Repo: "svc", Symbol: "a"},
		{Repo: "svc", Symbol: "b"},
		{Repo: "other", Symbol: "a"}, // same Symbol but different Repo — should keep
	}
	got := dedupByRepoSymbol(records)
	if len(got) != 3 {
		t.Fatalf("no dedup expected for distinct keys, got %d: %v", len(got), got)
	}
}

// TestDedupByRepoSymbol_PreservesOrder verifies that order is preserved and
// first occurrence wins across an interleaved duplicate pattern.
func TestDedupByRepoSymbol_PreservesOrder(t *testing.T) {
	records := []learnings.Record{
		{Repo: "svc", Symbol: "x", Note: "1st-x"},
		{Repo: "svc", Symbol: "y", Note: "1st-y"},
		{Repo: "svc", Symbol: "x", Note: "2nd-x"}, // dup, should be dropped
		{Repo: "svc", Symbol: "z", Note: "1st-z"},
		{Repo: "svc", Symbol: "y", Note: "2nd-y"}, // dup, should be dropped
	}
	got := dedupByRepoSymbol(records)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique records, got %d: %v", len(got), got)
	}
	// Order: x, y, z
	wantNotes := []string{"1st-x", "1st-y", "1st-z"}
	for i, r := range got {
		if r.Note != wantNotes[i] {
			t.Errorf("got[%d].Note = %q, want %q", i, r.Note, wantNotes[i])
		}
	}
}

// TestDedupByRepoSymbol_Empty verifies nil/empty input returns cleanly without panic.
func TestDedupByRepoSymbol_Empty(t *testing.T) {
	if got := dedupByRepoSymbol(nil); got != nil {
		t.Errorf("nil input: expected nil, got %v", got)
	}
	if got := dedupByRepoSymbol([]learnings.Record{}); len(got) != 0 {
		t.Errorf("empty input: expected empty, got %v", got)
	}
}
