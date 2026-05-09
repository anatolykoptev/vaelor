// cmd/go-code/tool_debug_investigate_upstream_test.go
//
// Unit tests for Sprint B2 upstream callgraph walk.
package main

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ---------------------------------------------------------------------------
// extractSymbolNameFromSubject
// ---------------------------------------------------------------------------

func TestExtractSymbolNameFromSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"foo in /path/to/file.rs:42", "foo"},
		{"accept_renegotiation_answer in /src/session.rs:100", "accept_renegotiation_answer"},
		{"GET /api/x", ""},
		{`operation "request"`, ""},
		{"", ""},
		{"justAName", ""},                        // no " in " → empty (can't confirm it's a symbol)
		{"GET /api/x in /src/handler.rs:10", ""}, // has " in " but first part has space → empty
		{"send_udp in /src/session.rs:226", "send_udp"},
	}
	for _, tc := range cases {
		got := extractSymbolNameFromSubject(tc.subject)
		if got != tc.want {
			t.Errorf("extractSymbolNameFromSubject(%q) = %q; want %q", tc.subject, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// flattenTraceTree
// ---------------------------------------------------------------------------

// makeSym is a helper to create a minimal *parser.Symbol.
func makeSym(name, file string, startLine, endLine uint32) *parser.Symbol {
	return &parser.Symbol{
		Name:      name,
		File:      file,
		StartLine: startLine,
		EndLine:   endLine,
		Kind:      parser.KindFunction,
	}
}

// TestFlattenCallers_SkipsRoot verifies that a single-element tree (root only,
// no children) returns an empty flat list.
func TestFlattenCallers_SkipsRoot(t *testing.T) {
	// Tree has one node: the root (the queried symbol itself), no children.
	root := callgraph.CallChainNode{Symbol: makeSym("target_fn", "/src/a.rs", 10, 20)}
	tree := []callgraph.CallChainNode{root}

	out := flattenTraceTree(tree, 0)
	if len(out) != 0 {
		t.Errorf("expected empty flat list for root-only tree, got %d nodes", len(out))
	}
}

// TestFlattenCallers_Depth verifies a two-level tree: root → callerA (depth 1) → callerB (depth 2).
func TestFlattenCallers_Depth(t *testing.T) {
	callerB := callgraph.CallChainNode{Symbol: makeSym("callerB", "/src/b.rs", 30, 40)}
	callerA := callgraph.CallChainNode{
		Symbol:   makeSym("callerA", "/src/a.rs", 15, 25),
		Children: []callgraph.CallChainNode{callerB},
	}
	root := callgraph.CallChainNode{
		Symbol:   makeSym("target_fn", "/src/t.rs", 5, 10),
		Children: []callgraph.CallChainNode{callerA},
	}
	tree := []callgraph.CallChainNode{root}

	out := flattenTraceTree(tree, 0)
	if len(out) != 2 {
		t.Fatalf("expected 2 flat nodes, got %d", len(out))
	}
	if out[0].symbol.Name != "callerA" || out[0].depth != 1 {
		t.Errorf("out[0]: want callerA@1, got %s@%d", out[0].symbol.Name, out[0].depth)
	}
	if out[1].symbol.Name != "callerB" || out[1].depth != 2 {
		t.Errorf("out[1]: want callerB@2, got %s@%d", out[1].symbol.Name, out[1].depth)
	}
}

// ---------------------------------------------------------------------------
// buildFakeCG builds a minimal CallGraph for testing. Edges: baz ← bar ← foo.
// ---------------------------------------------------------------------------

func buildFakeCG() *callgraph.CallGraph {
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

func bazHypothesis() investigate.Hypothesis {
	return investigate.Hypothesis{
		Subject:      "baz in /src/baz.rs:40",
		File:         "/src/baz.rs",
		Line:         40,
		EndLine:      50,
		AnomalyScore: 0.8,
		SpanCount:    5,
		Source:       "", // Tier-3: empty source
	}
}

// TestRunUpstreamPhase_AddsCallers verifies that callers of the top hypothesis
// are appended with Source=upstream_caller and correct AnomalyScores.
func TestRunUpstreamPhase_AddsCallers(t *testing.T) {
	cg := buildFakeCG()
	hyps := []investigate.Hypothesis{bazHypothesis()}

	result := runUpstreamPhase(context.Background(), cg, hyps, 3, 2)

	// Should have original + 2 callers (bar@depth1, foo@depth2).
	if len(result) != 3 {
		t.Fatalf("expected 3 hypotheses, got %d", len(result))
	}

	// Verify appended callers.
	bar := result[1]
	if bar.Source != investigate.HypothesisSourceUpstream {
		t.Errorf("bar.Source = %q; want %q", bar.Source, investigate.HypothesisSourceUpstream)
	}
	if bar.File != "/src/bar.rs" {
		t.Errorf("bar.File = %q; want /src/bar.rs", bar.File)
	}
	wantBarScore := 0.4 / 1.0 // depth 1
	if bar.AnomalyScore != wantBarScore {
		t.Errorf("bar.AnomalyScore = %f; want %f", bar.AnomalyScore, wantBarScore)
	}

	foo := result[2]
	if foo.Source != investigate.HypothesisSourceUpstream {
		t.Errorf("foo.Source = %q; want %q", foo.Source, investigate.HypothesisSourceUpstream)
	}
	wantFooScore := 0.4 / 2.0 // depth 2
	if foo.AnomalyScore != wantFooScore {
		t.Errorf("foo.AnomalyScore = %f; want %f", foo.AnomalyScore, wantFooScore)
	}
}

// TestRunUpstreamPhase_NilCG_NoOp verifies a nil callgraph returns hyps unchanged.
func TestRunUpstreamPhase_NilCG_NoOp(t *testing.T) {
	hyps := []investigate.Hypothesis{bazHypothesis()}
	result := runUpstreamPhase(context.Background(), nil, hyps, 3, 2)
	if len(result) != 1 {
		t.Errorf("expected 1 hypothesis with nil cg, got %d", len(result))
	}
}

// TestRunUpstreamPhase_DedupAgainstExisting verifies that if a caller symbol
// is already in the hypothesis pool, it is not added again.
func TestRunUpstreamPhase_DedupAgainstExisting(t *testing.T) {
	cg := buildFakeCG()

	// Pre-populate bar as existing hypothesis.
	barHyp := investigate.Hypothesis{
		Subject: "bar in /src/bar.rs:20",
		File:    "/src/bar.rs",
		Line:    20,
	}
	hyps := []investigate.Hypothesis{bazHypothesis(), barHyp}

	result := runUpstreamPhase(context.Background(), cg, hyps, 3, 2)

	// Should add only foo (bar already seen), so total = 3.
	if len(result) != 3 {
		t.Fatalf("expected 3 hypotheses (baz + bar + foo), got %d", len(result))
	}
	for _, h := range result {
		if h.File == "/src/bar.rs" && h.Source == investigate.HypothesisSourceUpstream {
			t.Error("bar added as upstream despite already existing")
		}
	}
}

// TestRunUpstreamPhase_TopNOnly verifies that only the first topN hypotheses
// are used as upstream-walk seeds.
func TestRunUpstreamPhase_TopNOnly(t *testing.T) {
	cg := buildFakeCG()

	// Two hypotheses: baz (index 0) and a non-callgraph one (index 1).
	// topN=1 → only baz is walked.
	hyps := []investigate.Hypothesis{
		bazHypothesis(),
		{Subject: "other in /src/other.rs:5", File: "/src/other.rs", Line: 5},
	}

	result := runUpstreamPhase(context.Background(), cg, hyps, 1, 2)

	// Walking only baz → adds bar + foo = total 4.
	if len(result) != 4 {
		t.Fatalf("topN=1: expected 4 hypotheses, got %d", len(result))
	}

	// With topN=0 → no walking → no additions.
	result2 := runUpstreamPhase(context.Background(), cg, hyps, 0, 2)
	if len(result2) != 2 {
		t.Fatalf("topN=0: expected 2 hypotheses, got %d", len(result2))
	}
}

// TestRunUpstreamPhase_GlobalCap verifies that additions are capped at 9
// total even when the fanin is larger.
func TestRunUpstreamPhase_GlobalCap(t *testing.T) {
	// Build a fat fanin: target ← c1, c2, ..., c15 (all at depth 1).
	target := makeSym("target", "/src/t.rs", 1, 5)
	syms := []*parser.Symbol{target}
	var edges []callgraph.CallEdge
	for i := 0; i < 15; i++ {
		caller := makeSym("caller", "/src/callers.rs", uint32(10+i), uint32(10+i+1))
		caller.Name = "caller" // Note: same name, different lines — key is Name@File
		// Make distinct by using unique file per caller.
		caller.File = "/src/caller_unique_file_that_is_long_enough_to_be_distinct_" + string(rune('a'+i)) + ".rs"
		caller.Name = "callerFn" + string(rune('a'+i))
		syms = append(syms, caller)
		edges = append(edges, callgraph.CallEdge{Caller: caller, Callee: target, CalleeName: "target"})
	}
	cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

	hyps := []investigate.Hypothesis{{
		Subject:      "target in /src/t.rs:1",
		File:         "/src/t.rs",
		Line:         1,
		AnomalyScore: 0.9,
	}}

	result := runUpstreamPhase(context.Background(), cg, hyps, 3, 2)

	// original (1) + capped additions (9) = 10 max.
	if len(result) > 10 {
		t.Errorf("expected ≤10 hypotheses (cap=9), got %d", len(result))
	}
	upstreamCount := 0
	for _, h := range result {
		if h.Source == investigate.HypothesisSourceUpstream {
			upstreamCount++
		}
	}
	if upstreamCount > 9 {
		t.Errorf("upstream additions capped at 9; got %d", upstreamCount)
	}
}

func TestFlattenCallers_SkipsCycleNodes(t *testing.T) {
	root := &parser.Symbol{Name: "root", File: "/r.go"}
	a := &parser.Symbol{Name: "A", File: "/a.go"}
	tree := []callgraph.CallChainNode{{
		Symbol: root,
		Children: []callgraph.CallChainNode{
			{Symbol: a},
			{Symbol: a, Cycle: true}, // cycle sentinel — must be skipped
		},
	}}
	got := flattenTraceTree(tree, 0)
	if len(got) != 1 || got[0].symbol.Name != "A" {
		t.Fatalf("expected single A non-cycle node, got %+v", got)
	}
}

func TestRunUpstreamPhase_SeedSourceSpan_Eligible(t *testing.T) {
	// Hypothesis with Source=HypothesisSourceSpan must be eligible as a seed.
	baz := makeSym("baz", "/baz.go", 1, 5)
	bar := makeSym("bar", "/bar.go", 10, 15)
	syms := []*parser.Symbol{baz, bar}
	edges := []callgraph.CallEdge{{Caller: bar, Callee: baz, CalleeName: "baz"}}
	cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}
	hyps := []investigate.Hypothesis{{
		Subject: "baz in /baz.go:1",
		File:    "/baz.go",
		Source:  investigate.HypothesisSourceSpan,
	}}
	out := runUpstreamPhase(context.Background(), cg, hyps, 3, 2)
	if len(out) <= 1 {
		t.Fatalf("expected upstream callers added for span-source hypothesis, got %d", len(out))
	}
}
