// cmd/go-code/tool_debug_investigate_downstream_test.go
//
// Unit tests for Sprint B4 downstream callgraph walk.
package main

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// buildFakeCGDown builds a minimal CallGraph for downstream testing.
// Edges: foo → bar → baz (foo calls bar, bar calls baz).
func buildFakeCGDown() *callgraph.CallGraph {
	foo := makeSym("foo", "/src/foo.rs", 1, 10)
	bar := makeSym("bar", "/src/bar.rs", 20, 30)
	baz := makeSym("baz", "/src/baz.rs", 40, 50)

	return &callgraph.CallGraph{
		Symbols: []*parser.Symbol{foo, bar, baz},
		Edges: []callgraph.CallEdge{
			{Caller: foo, Callee: bar, CalleeName: "bar"},
			{Caller: bar, Callee: baz, CalleeName: "baz"},
		},
	}
}

func fooHypothesis() investigate.Hypothesis {
	return investigate.Hypothesis{
		Subject:      "foo in /src/foo.rs:1",
		File:         "/src/foo.rs",
		Line:         1,
		EndLine:      10,
		AnomalyScore: 0.8,
		SpanCount:    5,
		Source:       "", // Tier-3: empty source
	}
}

// TestRunDownstreamPhase_AddsCallees verifies that callees of the top-1 hypothesis
// are appended with Source=downstream_callee and correct AnomalyScores.
func TestRunDownstreamPhase_AddsCallees(t *testing.T) {
	cg := buildFakeCGDown()
	hyps := []investigate.Hypothesis{fooHypothesis()}

	result := runDownstreamPhase(context.Background(), cg, hyps, 2)

	// Should have original + 2 callees (bar@depth1, baz@depth2).
	if len(result) != 3 {
		t.Fatalf("expected 3 hypotheses, got %d", len(result))
	}

	bar := result[1]
	if bar.Source != investigate.HypothesisSourceDownstream {
		t.Errorf("bar.Source = %q; want %q", bar.Source, investigate.HypothesisSourceDownstream)
	}
	if bar.File != "/src/bar.rs" {
		t.Errorf("bar.File = %q; want /src/bar.rs", bar.File)
	}
	wantBarScore := 0.3 / 1.0 // depth 1
	if bar.AnomalyScore != wantBarScore {
		t.Errorf("bar.AnomalyScore = %f; want %f", bar.AnomalyScore, wantBarScore)
	}

	baz := result[2]
	if baz.Source != investigate.HypothesisSourceDownstream {
		t.Errorf("baz.Source = %q; want %q", baz.Source, investigate.HypothesisSourceDownstream)
	}
	wantBazScore := 0.3 / 2.0 // depth 2
	if baz.AnomalyScore != wantBazScore {
		t.Errorf("baz.AnomalyScore = %f; want %f", baz.AnomalyScore, wantBazScore)
	}
}

// TestRunDownstreamPhase_NilCG_NoOp verifies a nil callgraph returns hyps unchanged.
func TestRunDownstreamPhase_NilCG_NoOp(t *testing.T) {
	hyps := []investigate.Hypothesis{fooHypothesis()}
	result := runDownstreamPhase(context.Background(), nil, hyps, 2)
	if len(result) != 1 {
		t.Errorf("expected 1 hypothesis with nil cg, got %d", len(result))
	}
}

// TestRunDownstreamPhase_DedupAgainstExisting verifies that a callee already
// in the hypothesis pool is not added again.
func TestRunDownstreamPhase_DedupAgainstExisting(t *testing.T) {
	cg := buildFakeCGDown()

	// Pre-populate bar as existing hypothesis.
	barHyp := investigate.Hypothesis{
		Subject: "bar in /src/bar.rs:20",
		File:    "/src/bar.rs",
		Line:    20,
	}
	hyps := []investigate.Hypothesis{fooHypothesis(), barHyp}

	result := runDownstreamPhase(context.Background(), cg, hyps, 2)

	// Should add only baz (bar already seen), so total = 3.
	if len(result) != 3 {
		t.Fatalf("expected 3 hypotheses (foo + bar + baz), got %d", len(result))
	}
	for _, h := range result {
		if h.File == "/src/bar.rs" && h.Source == investigate.HypothesisSourceDownstream {
			t.Error("bar added as downstream despite already existing")
		}
	}
}

// TestRunDownstreamPhase_Top1Only verifies only the first hypothesis is walked
// (not index 1 or beyond), matching the spec's top-1-only constraint.
func TestRunDownstreamPhase_Top1Only(t *testing.T) {
	cg := buildFakeCGDown()

	// Two hypotheses: baz (index 0 — no callees in this graph) and foo (index 1).
	// Only baz is walked; baz has no outgoing edges → no additions.
	hyps := []investigate.Hypothesis{
		{
			Subject:      "baz in /src/baz.rs:40",
			File:         "/src/baz.rs",
			Line:         40,
			EndLine:      50,
			AnomalyScore: 0.9,
		},
		fooHypothesis(), // index 1 — should NOT be walked
	}

	result := runDownstreamPhase(context.Background(), cg, hyps, 2)

	// baz has no outgoing edges, so no callees added.
	if len(result) != 2 {
		t.Errorf("top1-only: expected 2 hypotheses (baz only walked, no callees), got %d", len(result))
	}
}

// TestRunDownstreamPhase_GlobalCap verifies additions are capped at 9
// even when fan-out exceeds that.
func TestRunDownstreamPhase_GlobalCap(t *testing.T) {
	// Build fat fan-out: target → c1..c15 (each at depth 1).
	target := makeSym("target", "/src/t.rs", 1, 5)
	syms := []*parser.Symbol{target}
	var edges []callgraph.CallEdge
	for i := 0; i < 15; i++ {
		callee := &parser.Symbol{
			Name:      "callee" + string(rune('a'+i)),
			File:      "/src/callee_" + string(rune('a'+i)) + ".rs",
			StartLine: uint32(10 + i),
			EndLine:   uint32(10 + i + 1),
			Kind:      parser.KindFunction,
		}
		syms = append(syms, callee)
		edges = append(edges, callgraph.CallEdge{Caller: target, Callee: callee, CalleeName: callee.Name})
	}
	cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

	hyps := []investigate.Hypothesis{{
		Subject:      "target in /src/t.rs:1",
		File:         "/src/t.rs",
		Line:         1,
		AnomalyScore: 0.9,
	}}

	result := runDownstreamPhase(context.Background(), cg, hyps, 2)

	// original (1) + capped additions (9) = 10 max.
	if len(result) > 10 {
		t.Errorf("expected ≤10 hypotheses (cap=9), got %d", len(result))
	}
	downstreamCount := 0
	for _, h := range result {
		if h.Source == investigate.HypothesisSourceDownstream {
			downstreamCount++
		}
	}
	if downstreamCount > 9 {
		t.Errorf("downstream additions capped at 9; got %d", downstreamCount)
	}
}

// TestRunDownstreamPhase_NonSpanSource_Skipped verifies that when the top-1
// hypothesis has Source=upstream_caller (or any non-span non-empty source),
// no traversal occurs.
func TestRunDownstreamPhase_NonSpanSource_Skipped(t *testing.T) {
	cg := buildFakeCGDown()
	hyps := []investigate.Hypothesis{{
		Subject:      "foo in /src/foo.rs:1",
		File:         "/src/foo.rs",
		Line:         1,
		AnomalyScore: 0.9,
		Source:       investigate.HypothesisSourceUpstream, // non-span source → skip
	}}

	result := runDownstreamPhase(context.Background(), cg, hyps, 2)

	// No traversal: length unchanged.
	if len(result) != 1 {
		t.Errorf("expected 1 hypothesis (skipped non-span source), got %d", len(result))
	}
}

// TestRunDownstreamPhase_Depth1ScorePinned asserts that a depth=1 callee scores
// 0.3 (not 0.4, the upstream baseline). Keeps callee < caller < direct-symptom
// attribution priority intact.
//
// Load-bearing: callees less suspicious than direct symptom; do not normalize to 0.4.
func TestRunDownstreamPhase_Depth1ScorePinned(t *testing.T) {
	cg := buildFakeCGDown()
	hyps := []investigate.Hypothesis{fooHypothesis()}

	result := runDownstreamPhase(context.Background(), cg, hyps, 1)

	// depth=1 callee (bar) must be present.
	if len(result) < 2 {
		t.Fatalf("expected at least 2 hypotheses (foo + bar), got %d", len(result))
	}
	bar := result[1]
	const wantScore = 0.3
	if bar.AnomalyScore == 0.4 {
		t.Errorf("depth=1 callee score is 0.4 (upstream value) — must be 0.3 for callees")
	}
	if bar.AnomalyScore != wantScore {
		t.Errorf("bar.AnomalyScore = %f; want %f", bar.AnomalyScore, wantScore)
	}
}
