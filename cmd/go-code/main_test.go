package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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

// TestBuildInfoMetric asserts that the gocode_build_info gauge is registered
// in the default Prometheus registry with a non-empty git_sha label.
//
// Anti-tautology: without build_info_metric.go (or if resolveBuildSHA returns ""),
// no gauge is registered → the metric family is absent → the inner loop never
// executes → assert fires.
func TestBuildInfoMetric(t *testing.T) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("DefaultGatherer.Gather: %v", err)
	}
	var found bool
	var gitSHA string
	for _, mf := range mfs {
		if mf.GetName() != "gocode_build_info" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "git_sha" {
					gitSHA = lp.GetValue()
				}
			}
			if m.GetGauge() != nil && m.GetGauge().GetValue() == 1 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("gocode_build_info gauge missing or not equal to 1 — deploy provenance metric not registered at startup")
	}
	if gitSHA == "" {
		t.Fatal("gocode_build_info{git_sha} label is empty — SHA must be non-empty (or 'unknown' fallback)")
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

// TestToolTimeoutsCodeGraph asserts that code_graph has an explicit timeout
// in the ToolTimeouts map (2026-07-16 fix).
//
// Rationale: code_graph was absent from ToolTimeouts, silently inheriting the
// 90s harness default. On piter-now (1695 vertices) a Cypher query + LLM
// narrative took 1m30s — exactly hitting the 90s ceiling, causing the MCP
// client to drop the connection with Failed to connect even though the tool
// had completed successfully server-side. 120s gives headroom for the Cypher
// execution + LLM narrative generation on larger graphs.
//
// Anti-tautology: references the package-level toolTimeouts var. Fails if the
// entry is removed or changed below 120s.
func TestToolTimeoutsCodeGraph(t *testing.T) {
	d, ok := toolTimeouts["code_graph"]
	if !ok {
		t.Fatal("toolTimeouts[\"code_graph\"] is missing — code_graph has no explicit deadline; " +
			"see 2026-07-16: Cypher + narrative on 1695-vertex graph hit 90s default, MCP client dropped connection")
	}
	const min = 120 * time.Second
	if d < min {
		t.Errorf("toolTimeouts[\"code_graph\"] = %v, want >= %v", d, min)
	}
}

// TestToolTimeoutsCallTrace asserts that call_trace has at least 90s timeout
// (2026-07-16 fix: was 60s, not enough on cold cache parse + LLM narrative).
func TestToolTimeoutsCallTrace(t *testing.T) {
	d, ok := toolTimeouts["call_trace"]
	if !ok {
		t.Fatal("toolTimeouts[\"call_trace\"] is missing — call_trace has no explicit deadline")
	}
	const min = 90 * time.Second
	if d < min {
		t.Errorf("toolTimeouts[\"call_trace\"] = %v, want >= %v (cold cache parse + narrative)", d, min)
	}
}
