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
