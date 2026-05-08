package investigate

import (
	"sort"
	"testing"
)

func TestRankHypotheses_OrdersByCompositeScore(t *testing.T) {
	in := []Hypothesis{
		{Subject: "low_count_high_anomaly", SpanCount: 1, AnomalyScore: 0.9},
		{Subject: "high_count_low_anomaly", SpanCount: 100, AnomalyScore: 0.1},
		{Subject: "balanced", SpanCount: 10, AnomalyScore: 0.5},
		{Subject: "no_signal", SpanCount: 0, AnomalyScore: 0.0},
	}
	got := RankHypotheses(in)
	want := []string{"high_count_low_anomaly", "balanced", "low_count_high_anomaly", "no_signal"}
	for i, h := range got {
		if h.Subject != want[i] {
			t.Errorf("rank[%d]: got %q, want %q", i, h.Subject, want[i])
		}
	}
}

func TestRankHypotheses_StableOnTies(t *testing.T) {
	in := []Hypothesis{
		{Subject: "first", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "second", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "third", SpanCount: 5, AnomalyScore: 0.5},
	}
	got := RankHypotheses(in)
	if got[0].Subject != "first" || got[1].Subject != "second" || got[2].Subject != "third" {
		t.Errorf("not stable: %+v", got)
	}
}

func TestConfidenceFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  ConfidenceLevel
	}{
		{0.0, ConfidenceLow},
		{0.05, ConfidenceLow},
		{0.3, ConfidenceMedium},
		{0.6, ConfidenceMedium},
		{0.8, ConfidenceHigh},
		{1.5, ConfidenceHigh},
	}
	for _, c := range cases {
		if got := ConfidenceFromScore(c.score); got != c.want {
			t.Errorf("ConfidenceFromScore(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestInvestigationResult_StableSortPreserved(t *testing.T) {
	r := &InvestigationResult{
		Hypotheses: []Hypothesis{
			{Subject: "z", SpanCount: 1, AnomalyScore: 0.1},
			{Subject: "a", SpanCount: 10, AnomalyScore: 0.5},
		},
	}
	sort.SliceStable(r.Hypotheses, compositeLess(r.Hypotheses))
	if r.Hypotheses[0].Subject != "a" {
		t.Errorf("expected 'a' first by score, got %q", r.Hypotheses[0].Subject)
	}
}
