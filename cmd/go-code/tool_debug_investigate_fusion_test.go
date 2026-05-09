package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/investigate"
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
