package investigate

import (
	"testing"
)

func TestRankHypotheses_OrdersByCompositeScore(t *testing.T) {
	// Hand-trace under LinearMinMax:
	// SpanCount: [1, 100, 10, 0]. min=0, max=100. Norm: [0.01, 1.0, 0.1, 0.0].
	// AnomalyScore: [0.9, 0.1, 0.5, 0.0]. min=0, max=0.9. Norm: [1.0, 0.111, 0.555, 0.0].
	// Fused (sum, eq weights): [1.01, 1.111, 0.655, 0.0].
	// Order: high_count_low_anomaly > low_count_high_anomaly > balanced > no_signal.
	in := []Hypothesis{
		{Subject: "low_count_high_anomaly", SpanCount: 1, AnomalyScore: 0.9},
		{Subject: "high_count_low_anomaly", SpanCount: 100, AnomalyScore: 0.1},
		{Subject: "balanced", SpanCount: 10, AnomalyScore: 0.5},
		{Subject: "no_signal", SpanCount: 0, AnomalyScore: 0.0},
	}
	got := RankHypotheses(in)
	want := []string{"high_count_low_anomaly", "low_count_high_anomaly", "balanced", "no_signal"}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i, h := range got {
		if h.Subject != want[i] {
			t.Errorf("rank[%d]: got %q, want %q", i, h.Subject, want[i])
		}
	}
}

func TestRankHypotheses_StableOnTies(t *testing.T) {
	// Flat lists: all SpanCount=5, all AnomalyScore=0.5 — LinearMinMax max==min
	// for both lists → fused=0 for all → stable first-seen order preserved.
	in := []Hypothesis{
		{Subject: "first", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "second", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "third", SpanCount: 5, AnomalyScore: 0.5},
	}
	got := RankHypotheses(in)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	if got[0].Subject != "first" || got[1].Subject != "second" || got[2].Subject != "third" {
		t.Errorf("not stable: %+v", got)
	}
}

func TestRankHypotheses_PreservesCallerConfidence(t *testing.T) {
	in := []Hypothesis{
		{Subject: "preset_high", SpanCount: 1, AnomalyScore: 0.1, Confidence: ConfidenceHigh},
		{Subject: "no_preset", SpanCount: 100, AnomalyScore: 0.9},
		{Subject: "preset_low_huge", SpanCount: 5000, AnomalyScore: 1.0, Confidence: ConfidenceLow},
	}
	got := RankHypotheses(in)
	for _, h := range got {
		switch h.Subject {
		case "preset_high":
			if h.Confidence != ConfidenceHigh {
				t.Errorf("preset_high: got %q, want preserved %q", h.Confidence, ConfidenceHigh)
			}
		case "preset_low_huge":
			if h.Confidence != ConfidenceLow {
				t.Errorf("preset_low_huge: got %q, want preserved %q (caller override survives extreme data)", h.Confidence, ConfidenceLow)
			}
		case "no_preset":
			if h.Confidence == "" {
				t.Error("no_preset: expected Confidence filled by heuristic, got empty")
			}
		}
	}
}

func TestRankHypotheses_EmptyInput(t *testing.T) {
	if got := RankHypotheses(nil); got != nil {
		t.Errorf("expected nil for nil input, got %+v", got)
	}
	if got := RankHypotheses([]Hypothesis{}); got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestRankHypotheses_SingleElement(t *testing.T) {
	in := []Hypothesis{{Subject: "only", SpanCount: 100, AnomalyScore: 0.5}}
	got := RankHypotheses(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 element, got %d", len(got))
	}
	// Single element: max==min for both lists → fused=0 → ConfidenceLow
	if got[0].Confidence != ConfidenceLow {
		t.Errorf("single element: got %q, want %q (fused=0 from flat list)", got[0].Confidence, ConfidenceLow)
	}
}

func TestConfidenceFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  ConfidenceLevel
	}{
		{0.0, ConfidenceLow},
		{0.05, ConfidenceLow},
		{0.19999, ConfidenceLow},
		{0.2, ConfidenceMedium},
		{0.3, ConfidenceMedium},
		{0.6, ConfidenceMedium},
		{0.69999, ConfidenceMedium},
		{0.7, ConfidenceHigh},
		{0.8, ConfidenceHigh},
		{1.5, ConfidenceHigh},
	}
	for _, c := range cases {
		if got := ConfidenceFromScore(c.score); got != c.want {
			t.Errorf("ConfidenceFromScore(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

// ---------- γ.E.2: NextCheck struct tests ----------

// TestNextCheck_StructFields verifies NextCheck has Tool and Args fields.
func TestNextCheck_StructFields(t *testing.T) {
	nc := NextCheck{
		Tool: "understand",
		Args: map[string]string{"symbol": "HandleMessage", "repo": "/host/src/go-code"},
	}
	if nc.Tool != "understand" {
		t.Errorf("Tool = %q, want 'understand'", nc.Tool)
	}
	if nc.Args["symbol"] != "HandleMessage" {
		t.Errorf("Args[symbol] = %q, want 'HandleMessage'", nc.Args["symbol"])
	}
}

// TestNextCheck_EmptyArgs is valid (tool-only recommendation).
func TestNextCheck_EmptyArgs(t *testing.T) {
	nc := NextCheck{Tool: "code_health"}
	if nc.Tool != "code_health" {
		t.Errorf("Tool = %q, want 'code_health'", nc.Tool)
	}
	if len(nc.Args) != 0 {
		t.Errorf("expected empty Args, got %v", nc.Args)
	}
}

// TestHypothesis_NextChecksIsStructured verifies NextChecks is []NextCheck, not []string.
func TestHypothesis_NextChecksIsStructured(t *testing.T) {
	h := Hypothesis{
		Subject: "handleRequest",
		NextChecks: []NextCheck{
			{Tool: "understand", Args: map[string]string{"symbol": "handleRequest", "repo": "/src"}},
			{Tool: "code_health"},
		},
	}
	if len(h.NextChecks) != 2 {
		t.Fatalf("expected 2 NextChecks, got %d", len(h.NextChecks))
	}
	if h.NextChecks[0].Tool != "understand" {
		t.Errorf("NextChecks[0].Tool = %q, want 'understand'", h.NextChecks[0].Tool)
	}
	if h.NextChecks[1].Tool != "code_health" {
		t.Errorf("NextChecks[1].Tool = %q, want 'code_health'", h.NextChecks[1].Tool)
	}
}
