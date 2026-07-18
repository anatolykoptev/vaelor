package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/investigate"
)

func TestRunFusionRank_MergesSignals(t *testing.T) {
	// h0: low anomaly, high impact
	// h1: high anomaly, low impact
	// h2: very high anomaly, medium impact
	// After fusion with weights: anomaly=0.40 dominates, so h2 > h1 > h0 expected.
	hyps := []investigate.Hypothesis{
		{Subject: "h0", AnomalyScore: 0.1, Impact: &investigate.ImpactInfo{DirectCallers: 20}},
		{Subject: "h1", AnomalyScore: 0.8, Impact: &investigate.ImpactInfo{DirectCallers: 1}},
		{Subject: "h2", AnomalyScore: 1.0, Impact: &investigate.ImpactInfo{DirectCallers: 5}},
	}

	recentCommits := map[string]int{}
	historicalSubjects := map[string]bool{}

	ranked := runFusionRank(hyps, recentCommits, historicalSubjects)
	if len(ranked) != 3 {
		t.Fatalf("got %d ranked, want 3", len(ranked))
	}
	if ranked[0].Subject != "h2" {
		t.Errorf("top hypothesis = %q, want h2", ranked[0].Subject)
	}
}

func TestRunFusionRank_NoSignals_NoOp(t *testing.T) {
	hyps := []investigate.Hypothesis{}
	ranked := runFusionRank(hyps, nil, nil)
	if len(ranked) != 0 {
		t.Errorf("expected empty, got %d", len(ranked))
	}
}

func TestSignalBreakdown_AllSignalsPresent(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "a", AnomalyScore: 0.5, Impact: &investigate.ImpactInfo{DirectCallers: 3}},
		{Subject: "b", AnomalyScore: 0.3, Impact: &investigate.ImpactInfo{DirectCallers: 1}},
	}
	recentCommits := map[string]int{"": 2}
	historicalSubjects := map[string]bool{"a": true}

	ranked := runFusionRank(hyps, recentCommits, historicalSubjects)
	for _, h := range ranked {
		for _, sig := range []string{"metric_anomaly", "recency", "complexity", "impact", "historical"} {
			if _, ok := h.SignalBreakdown[sig]; !ok {
				t.Errorf("hypothesis %q missing signal %q in breakdown", h.Subject, sig)
			}
		}
	}
}

// ---- format tests ----

func TestFormat_FusedScore_NotZero_Rendered(t *testing.T) {
	r := buildMinimalResult()
	r.Hypotheses[0].FusedScore = 0.82
	r.Hypotheses[0].SignalBreakdown = map[string]float64{
		"metric_anomaly": 0.90,
		"recency":        0.50,
		"complexity":     0.30,
		"impact":         0.85,
		"historical":     0.00,
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, "fused_score") {
		t.Errorf("expected <fused_score> block in output; got:\n%s", out)
	}
	if !strings.Contains(out, "metric_anomaly") {
		t.Errorf("expected signal metric_anomaly in output; got:\n%s", out)
	}
}

func TestFormat_FusedScore_Zero_Skipped(t *testing.T) {
	r := buildMinimalResult()
	r.Hypotheses[0].FusedScore = 0.0
	out := formatInvestigationResult(r)
	if strings.Contains(out, "fused_score") {
		t.Errorf("expected no <fused_score> when FusedScore==0; got:\n%s", out)
	}
}

func TestFormat_RecentChange_Empty_Skipped(t *testing.T) {
	r := buildMinimalResult()
	r.Hypotheses[0].RecentChange = nil
	out := formatInvestigationResult(r)
	if strings.Contains(out, "recent_change") {
		t.Errorf("expected no <recent_change> when nil; got:\n%s", out)
	}
}

func TestFormat_RecentChange_NotEmpty_Rendered(t *testing.T) {
	r := buildMinimalResult()
	r.Hypotheses[0].RecentChange = &investigate.RecentChange{
		File:  "src/foo.go",
		Since: "2026-04-09",
		Diff:  "+ foo()\n- bar()\n",
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, "recent_change") {
		t.Errorf("expected <recent_change> block; got:\n%s", out)
	}
	if !strings.Contains(out, "src/foo.go") {
		t.Errorf("expected file in output; got:\n%s", out)
	}
}

func buildMinimalResult() *investigate.InvestigationResult {
	return &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "test subject", AnomalyScore: 0.5},
		},
	}
}

// TestWrapCDATA_SplitsCloseSeq verifies that literal "]]>" in a diff is split
// so it cannot terminate an enclosing CDATA section. (The formatter now routes
// CDATA payloads through the shared wrapCDATA helper; the prior escapeCDATA
// duplicate was removed.)
func TestWrapCDATA_SplitsCloseSeq(t *testing.T) {
	in := "before ]]> after"
	got := wrapCDATA(in)
	want := "<![CDATA[before ]]]]><![CDATA[> after]]>"
	if got != want {
		t.Errorf("wrapCDATA(%q) = %q, want %q", in, got, want)
	}
}

// TestWrapCDATA_NoOp_WhenNoCloseSeq verifies strings without "]]>" are wrapped unchanged.
func TestWrapCDATA_NoOp_WhenNoCloseSeq(t *testing.T) {
	in := "plain diff text\n+ added\n- removed"
	got := wrapCDATA(in)
	want := "<![CDATA[" + in + "]]>"
	if got != want {
		t.Errorf("wrapCDATA(%q) = %q, want %q", in, got, want)
	}
}

// TestFusionRankAndBreakdownAgree verifies that each hypothesis's FusedScore equals
// the weighted sum of its SignalBreakdown values. This is a regression guard: if
// ranking.FusionRank ever changes its normalization (e.g. softmax migration), this
// test catches divergence between the two code paths.
func TestFusionRankAndBreakdownAgree(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "A", AnomalyScore: 0.9},
		{Subject: "B", AnomalyScore: 0.5},
		{Subject: "C", AnomalyScore: 0.1},
	}
	weights := map[string]float64{
		fusionSigMetricAnomaly: 0.40,
		fusionSigRecency:       0.20,
		fusionSigComplexity:    0.15,
		fusionSigImpact:        0.15,
		fusionSigHistorical:    0.10,
	}

	out := runFusionRank(hyps, nil, nil)

	const tolerance = 1e-6
	for _, h := range out {
		if h.FusedScore == 0 {
			continue
		}
		var manual float64
		for sigName, normVal := range h.SignalBreakdown {
			manual += normVal * weights[sigName]
		}
		if diff := manual - h.FusedScore; diff > tolerance || diff < -tolerance {
			t.Errorf("hypothesis %q: breakdown weighted-sum %.8f != FusedScore %.8f (diff=%.8f)",
				h.Subject, manual, h.FusedScore, diff)
		}
	}
}

// TestRunFusionRank_TieBreakBySubject verifies that hypotheses with identical FusedScore
// are sorted lexicographically by Subject — ensuring cache key stability across calls
// when map iteration order is non-deterministic.
func TestRunFusionRank_TieBreakBySubject(t *testing.T) {
	// Use identical AnomalyScore and no other signals so all hyps get the same FusedScore.
	hyps := []investigate.Hypothesis{
		{Subject: "zebra", AnomalyScore: 0.5},
		{Subject: "apple", AnomalyScore: 0.5},
		{Subject: "mango", AnomalyScore: 0.5},
	}
	ranked := runFusionRank(hyps, nil, nil)
	if len(ranked) != 3 {
		t.Fatalf("got %d ranked, want 3", len(ranked))
	}
	// All FusedScores equal → lexicographic Subject order expected.
	subjects := []string{ranked[0].Subject, ranked[1].Subject, ranked[2].Subject}
	want := []string{"apple", "mango", "zebra"}
	for i := range want {
		if subjects[i] != want[i] {
			t.Errorf("ranked[%d].Subject = %q, want %q (full order: %v)", i, subjects[i], want[i], subjects)
		}
	}
}
