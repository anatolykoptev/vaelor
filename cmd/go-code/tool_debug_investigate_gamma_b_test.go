// cmd/go-code/tool_debug_investigate_gamma_b_test.go
// Tests for Phase γ.B enrichment: dead-code filter, impact, symbol body.
package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/investigate"
)

// ---------- γ.B.1 dead-code filter ----------

// TestDeadCodeFilter_DropsDeadHypothesis asserts that a hypothesis whose
// resolved symbol name is in the dead-code set is removed from the output,
// and the HypothesesDroppedAsDead counter increments.
func TestDeadCodeFilter_DropsDeadHypothesis(t *testing.T) {
	input := []investigate.Hypothesis{
		{Subject: "DeadFn in some/file.go", File: "some/file.go", Line: 10, SpanCount: 5},
		{Subject: "LiveFn in some/other.go", File: "some/other.go", Line: 20, SpanCount: 3},
	}
	deadSet := map[string]bool{"DeadFn": true}
	diag := &investigate.Diagnostics{}

	result := filterDeadHypotheses(input, deadSet, diag)

	if len(result) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(result))
	}
	if !strings.Contains(result[0].Subject, "LiveFn") {
		t.Errorf("surviving hypothesis should contain LiveFn, got %q", result[0].Subject)
	}
	if diag.HypothesesDroppedAsDead != 1 {
		t.Errorf("expected HypothesesDroppedAsDead=1, got %d", diag.HypothesesDroppedAsDead)
	}
}

// TestDeadCodeFilter_KeepsHypothesisWithNoFile asserts that hypotheses
// without file resolution (File == "") are never dropped by dead filter.
func TestDeadCodeFilter_KeepsHypothesisWithNoFile(t *testing.T) {
	input := []investigate.Hypothesis{
		{Subject: `operation "DeadFn"`, File: "", Line: 0, SpanCount: 5},
	}
	deadSet := map[string]bool{"DeadFn": true}
	diag := &investigate.Diagnostics{}

	result := filterDeadHypotheses(input, deadSet, diag)

	if len(result) != 1 {
		t.Fatalf("expected 1 (no-file hyp kept), got %d", len(result))
	}
	if diag.HypothesesDroppedAsDead != 0 {
		t.Errorf("expected no drops for unresolved hypothesis, got %d", diag.HypothesesDroppedAsDead)
	}
}

// TestDeadCodeFilter_AllAlive asserts empty dead set drops nothing.
func TestDeadCodeFilter_AllAlive(t *testing.T) {
	input := []investigate.Hypothesis{
		{Subject: "FuncA", File: "a.go", Line: 1},
		{Subject: "FuncB", File: "b.go", Line: 2},
	}
	diag := &investigate.Diagnostics{}

	result := filterDeadHypotheses(input, map[string]bool{}, diag)

	if len(result) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(result))
	}
	if diag.HypothesesDroppedAsDead != 0 {
		t.Errorf("expected 0 drops, got %d", diag.HypothesesDroppedAsDead)
	}
}

// TestFilterDeadHypotheses_EmitsWarning asserts that each dropped hypothesis
// produces a Warning entry in Diagnostics with subject and file info.
func TestFilterDeadHypotheses_EmitsWarning(t *testing.T) {
	input := []investigate.Hypothesis{
		{Subject: "DeadFn in some/file.go", File: "some/file.go", Line: 10},
		{Subject: "LiveFn in other.go", File: "other.go", Line: 20},
	}
	deadSet := map[string]bool{"DeadFn": true}
	diag := &investigate.Diagnostics{}

	_ = filterDeadHypotheses(input, deadSet, diag)

	if len(diag.Warnings) != 1 {
		t.Fatalf("expected 1 Warning, got %d: %v", len(diag.Warnings), diag.Warnings)
	}
	if !strings.Contains(diag.Warnings[0], "DeadFn") {
		t.Errorf("Warning should mention subject, got %q", diag.Warnings[0])
	}
	if !strings.Contains(diag.Warnings[0], "some/file.go") {
		t.Errorf("Warning should mention file, got %q", diag.Warnings[0])
	}
}

// ---------- γ.B.2 impact enrichment ----------

// TestImpactPhase_EnrichesTopThree asserts that runImpactPhase fills Impact
// on hypotheses [0..2] and leaves [3+] nil.
func TestImpactPhase_EnrichesTopThree(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "Fn1", File: "f1.go", Line: 1},
		{Subject: "Fn2", File: "f2.go", Line: 2},
		{Subject: "Fn3", File: "f3.go", Line: 3},
		{Subject: "Fn4", File: "f4.go", Line: 4},
	}
	// stub injector: returns a fixed ImpactInfo for any subject
	stub := func(subject string) *investigate.ImpactInfo {
		return &investigate.ImpactInfo{
			DirectCallers: 5,
			TotalAffected: 20,
			BlastRadius:   "medium",
			RiskScore:     4.2,
		}
	}

	result := applyImpactStub(hyps, stub, 3)

	for i := 0; i < 3; i++ {
		if result[i].Impact == nil {
			t.Errorf("hypothesis[%d] should have Impact, got nil", i)
		}
	}
	if result[3].Impact != nil {
		t.Errorf("hypothesis[3] should have nil Impact (beyond top-3)")
	}
}

// TestImpactPhase_SkipsNilResult asserts that nil return from injector
// leaves hypothesis.Impact nil.
func TestImpactPhase_SkipsNilResult(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "Fn1", File: "f1.go", Line: 1},
	}
	stub := func(_ string) *investigate.ImpactInfo { return nil }

	result := applyImpactStub(hyps, stub, 3)

	if result[0].Impact != nil {
		t.Errorf("expected nil Impact when stub returns nil")
	}
}

// ---------- γ.B.3 symbol body ----------

// TestSymbolBodyPhase_EnrichesTopOne asserts that applySymbolBody sets
// SymbolBody on hypothesis[0] only.
func TestSymbolBodyPhase_EnrichesTopOne(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "Fn1", File: "f1.go", Line: 1},
		{Subject: "Fn2", File: "f2.go", Line: 2},
	}
	stub := func(_ string) *investigate.SymbolBodyInfo {
		return &investigate.SymbolBodyInfo{ErrorExits: 3, HasDeferCleanup: true}
	}

	result := applySymbolBodyStub(hyps, stub)

	if result[0].SymbolBody == nil {
		t.Errorf("hypothesis[0] should have SymbolBody")
	}
	if result[0].SymbolBody.ErrorExits != 3 {
		t.Errorf("expected ErrorExits=3, got %d", result[0].SymbolBody.ErrorExits)
	}
	if result[1].SymbolBody != nil {
		t.Errorf("hypothesis[1] should have nil SymbolBody (not top-1)")
	}
}

// ---------- γ.B format ----------

// TestFormatHypothesis_RendersImpact asserts the impact XML block renders.
func TestFormatHypothesis_RendersImpact(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject: "Fn1",
				File:    "f1.go",
				Line:    1,
				Impact: &investigate.ImpactInfo{
					DirectCallers: 10,
					TotalAffected: 50,
					BlastRadius:   "high",
					RiskScore:     12.5,
				},
			},
		},
	}

	out := formatInvestigationResult(r)
	if !strings.Contains(out, `direct_callers="10"`) {
		t.Errorf("expected direct_callers in output, got:\n%s", out)
	}
	if !strings.Contains(out, `blast_radius="high"`) {
		t.Errorf("expected blast_radius in output, got:\n%s", out)
	}
}

// TestFormatHypothesis_RendersSymbolBody asserts symbol_body XML block renders.
func TestFormatHypothesis_RendersSymbolBody(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject: "Fn1",
				File:    "f1.go",
				Line:    1,
				SymbolBody: &investigate.SymbolBodyInfo{
					ErrorExits:      3,
					HasDeferCleanup: true,
					HasTODO:         false,
				},
			},
		},
	}

	out := formatInvestigationResult(r)
	if !strings.Contains(out, `error_exits="3"`) {
		t.Errorf("expected error_exits in output, got:\n%s", out)
	}
	if !strings.Contains(out, `has_defer="true"`) {
		t.Errorf("expected has_defer in output, got:\n%s", out)
	}
}
