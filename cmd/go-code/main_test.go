package main

import (
	"testing"
	"time"
)

// TestToolTimeoutsUnderstand asserts that the understand tool has an explicit
// timeout in the ToolTimeouts map.
//
// Rationale: understand was absent from ToolTimeouts, causing it to silently
// ride the 90s harness default. On the 2026-05-27 incident a not-found symbol
// lookup hit a dead embed server and blocked the full 90s deadline.
// 30s is sufficient: BFS call graph + AGE lookups + 5s embed cap (Fix #2).
//
// Anti-tautology: references the package-level toolTimeouts var (the deployed
// config) — not a local copy. This test fails if the entry is removed or
// changed, catching regression at CI rather than at production incident time.
func TestToolTimeoutsUnderstand(t *testing.T) {
	d, ok := toolTimeouts["understand"]
	if !ok {
		t.Fatal("toolTimeouts[\"understand\"] is missing — understand tool has no explicit deadline; " +
			"see 2026-05-27 incident: not-found symbol + dead embed server blocked 90s harness default")
	}
	const want = 30 * time.Second
	if d != want {
		t.Errorf("toolTimeouts[\"understand\"] = %v, want %v", d, want)
	}
}

// TestToolTimeoutsSemanticSearch asserts that semantic_search has an explicit
// timeout in the ToolTimeouts map (Bug #2 fix, 2026-06-02).
//
// Rationale: semantic_search was absent from ToolTimeouts, silently inheriting
// the 90s harness default. Although IndexRepoAsync detaches from the request
// context (using context.Background()), the embed query + store search leg of
// the tool itself can block the connection open for the full 90s if the embed
// server is unresponsive. 30s caps the synchronous leg while leaving the
// background index unaffected.
//
// Anti-tautology: this test fails if semantic_search is removed from the map
// or its deadline is changed, catching regressions before they reach production.
func TestToolTimeoutsSemanticSearch(t *testing.T) {
	d, ok := toolTimeouts["semantic_search"]
	if !ok {
		t.Fatal("toolTimeouts[\"semantic_search\"] is missing — semantic_search has no explicit deadline; " +
			"see Bug #2 (2026-06-02): missing entry lets clients hold connection for 90s harness default")
	}
	const want = 30 * time.Second
	if d != want {
		t.Errorf("toolTimeouts[\"semantic_search\"] = %v, want %v", d, want)
	}
}
