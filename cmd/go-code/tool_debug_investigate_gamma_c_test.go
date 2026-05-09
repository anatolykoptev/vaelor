// cmd/go-code/tool_debug_investigate_gamma_c_test.go
// Tests for Phase γ.C: historical incidents + hint-driven candidate hypotheses.
package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// ---------- γ.C.1 helpers ----------

// TestRiskLevelFromScore verifies boundary cases:
//   - score < 0.5  → "low"
//   - score == 0.5 → "medium"
//   - 0.5 ≤ score < 0.8 → "medium"
//   - score == 0.8 → "high"
//   - score > 0.8  → "high"
func TestRiskLevelFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.0, "low"},
		{0.3, "low"},
		{0.499, "low"},
		{0.5, "medium"},
		{0.6, "medium"},
		{0.799, "medium"},
		{0.8, "high"},
		{0.9, "high"},
		{1.0, "high"},
	}
	for _, tc := range cases {
		got := riskLevelFromScore(tc.score)
		if got != tc.want {
			t.Errorf("riskLevelFromScore(%.3f) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

// TestPrimarySpikeKind_Empty verifies empty spikes returns "".
func TestPrimarySpikeKind_Empty(t *testing.T) {
	got := primarySpikeKind(nil)
	if got != "" {
		t.Errorf("primarySpikeKind(nil) = %q, want %q", got, "")
	}
	got = primarySpikeKind([]investigate.MetricSpike{})
	if got != "" {
		t.Errorf("primarySpikeKind([]) = %q, want %q", got, "")
	}
}

// TestPrimarySpikeKind_FirstKind verifies first spike Kind is returned.
func TestPrimarySpikeKind_FirstKind(t *testing.T) {
	spikes := []investigate.MetricSpike{
		{Kind: "latency"},
		{Kind: "failure"},
	}
	got := primarySpikeKind(spikes)
	if got != "latency" {
		t.Errorf("primarySpikeKind = %q, want %q", got, "latency")
	}
}

// TestTruncate verifies string truncation and pass-through.
func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short string: got %q", got)
	}
	long := strings.Repeat("x", 20)
	got := truncate(long, 10)
	if len(got) != 10 {
		t.Errorf("truncate(len=20, n=10) → len=%d, want 10", len(got))
	}
	if got != strings.Repeat("x", 10) {
		t.Errorf("truncate content wrong: %q", got)
	}
}

// TestRunHistoryPersist_NoStore verifies nil store is a no-op (no panic).
func TestRunHistoryPersist_NoStore(t *testing.T) {
	res := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "SomeFunc", AnomalyScore: 0.9},
		},
		LLMSummary: "test summary",
	}
	// Must not panic with nil store.
	runHistoryPersist(t.Context(), nil, "test-svc", 0.9, res)
	if res.Diagnostics.LearningsPersisted {
		t.Error("LearningsPersisted should remain false with nil store")
	}
}

// TestRunHistoryPersist_NamespacePrefix verifies the Flag has "investigate:" prefix.
// Uses a stub to capture the persisted record without requiring a real DB.
func TestRunHistoryPersist_NamespacePrefix(t *testing.T) {
	// We test the flag-building logic by examining buildInvestigateRecord directly.
	res := &investigate.InvestigationResult{
		Service: "svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "HandleReq in handler.go", AnomalyScore: 0.85},
		},
		LLMSummary: "error spike in handler",
		MetricSpikes: []investigate.MetricSpike{
			{Kind: "failure"},
		},
	}
	rec := buildInvestigateRecord("svc", 0.85, res)
	if !strings.HasPrefix(rec.Flag, "investigate:") {
		t.Errorf("Flag %q should start with 'investigate:'", rec.Flag)
	}
	if rec.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want 'high'", rec.RiskLevel)
	}
	if rec.Symbol != "HandleReq in handler.go" {
		t.Errorf("Symbol = %q, want 'HandleReq in handler.go'", rec.Symbol)
	}
	if rec.Repo != "svc" {
		t.Errorf("Repo = %q, want 'svc'", rec.Repo)
	}
}

// ---------- γ.C.2 historical incidents retrieval ----------

// TestRetrieveHistoricalIncidents_FiltersReviewRecords verifies only records
// with "investigate:" flag prefix are surfaced, dropping review-side records.
func TestRetrieveHistoricalIncidents_FiltersReviewRecords(t *testing.T) {
	from := []investigate.HistoricalIncident{
		{Flag: "investigate:latency", Symbol: "HandleMsg"},
		{Flag: "policy:forbidden_import", Symbol: "ImportBad"},
		{Flag: "investigate:failure", Symbol: "ConnPool"},
		{Flag: "", Symbol: "Unknown"},
	}
	filtered := filterInvestigateIncidents(from)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 investigate: records, got %d: %v", len(filtered), filtered)
	}
	for _, r := range filtered {
		if !strings.HasPrefix(r.Flag, "investigate:") {
			t.Errorf("non-investigate record leaked: %+v", r)
		}
	}
}

// TestRetrieveHistoricalIncidents_EmptyResult verifies empty slice on no results.
func TestRetrieveHistoricalIncidents_EmptyResult(t *testing.T) {
	filtered := filterInvestigateIncidents(nil)
	if filtered != nil {
		t.Errorf("expected nil, got %v", filtered)
	}
}

// ---------- γ.C.3 hint-driven search ----------

// TestHintDrivenSearch_AddsHypotheses verifies codesearch matches are merged
// into hypotheses with Source="hint_match".
func TestHintDrivenSearch_AddsHypotheses(t *testing.T) {
	fakeMatches := []hintSearchMatch{
		{File: "pkg/handler.go", Line: 42, Text: "func HandleReq("},
		{File: "pkg/worker.go", Line: 10, Text: "func runWorker("},
	}
	hyps := applyHintMatches(nil, fakeMatches)
	if len(hyps) != 2 {
		t.Fatalf("expected 2 hypotheses from hint matches, got %d", len(hyps))
	}
	for _, h := range hyps {
		if h.Source != "hint_match" {
			t.Errorf("hypothesis Source = %q, want 'hint_match'", h.Source)
		}
		if h.AnomalyScore != 0.5 {
			t.Errorf("hypothesis AnomalyScore = %.2f, want 0.5", h.AnomalyScore)
		}
	}
	// File should be the match File, not modified.
	if hyps[0].File != "pkg/handler.go" {
		t.Errorf("hypothesis[0].File = %q, want pkg/handler.go", hyps[0].File)
	}
	if hyps[0].Line != 42 {
		t.Errorf("hypothesis[0].Line = %d, want 42", hyps[0].Line)
	}
}

// TestHintDrivenSearch_EmptyHint_NoOp verifies empty hint produces no hypotheses.
func TestHintDrivenSearch_EmptyHint_NoOp(t *testing.T) {
	hyps := applyHintMatches(nil, nil)
	if len(hyps) != 0 {
		t.Errorf("expected 0 hypotheses with empty hint, got %d", len(hyps))
	}
}

// TestHintDrivenSearch_MergesIntoExisting verifies hint matches are appended
// to existing hypotheses without clobbering them.
func TestHintDrivenSearch_MergesIntoExisting(t *testing.T) {
	existing := []investigate.Hypothesis{
		{Subject: "existing op", Source: "span"},
	}
	fakeMatches := []hintSearchMatch{
		{File: "a.go", Line: 1, Text: "func foo("},
	}
	hyps := applyHintMatches(existing, fakeMatches)
	if len(hyps) != 2 {
		t.Fatalf("expected 2 (1 existing + 1 hint), got %d", len(hyps))
	}
	if hyps[0].Source != "span" {
		t.Errorf("existing hypothesis Source corrupted: %q", hyps[0].Source)
	}
	if hyps[1].Source != "hint_match" {
		t.Errorf("hint hypothesis Source = %q, want 'hint_match'", hyps[1].Source)
	}
}

// ---------- γ.C format ----------

// TestFormatHistoricalIncidents_RendersBlock verifies XML block is rendered
// when HistoricalIncidents is non-empty.
func TestFormatHistoricalIncidents_RendersBlock(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		HistoricalIncidents: []investigate.HistoricalIncident{
			{
				Repo:      "test-svc",
				Symbol:    "HandleMessage",
				RiskLevel: "high",
				Flag:      "investigate:latency",
				Note:      "past summary text",
			},
		},
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, "<historical_incidents>") {
		t.Errorf("expected <historical_incidents> block, got:\n%s", out)
	}
	if !strings.Contains(out, `symbol="HandleMessage"`) {
		t.Errorf("expected symbol attribute, got:\n%s", out)
	}
	if !strings.Contains(out, "past summary text") {
		t.Errorf("expected note text, got:\n%s", out)
	}
}

// TestFormatHistoricalIncidents_SkipsEmpty verifies no XML block when empty.
func TestFormatHistoricalIncidents_SkipsEmpty(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service:             "test-svc",
		HistoricalIncidents: nil,
	}
	out := formatInvestigationResult(r)
	if strings.Contains(out, "historical_incidents") {
		t.Errorf("unexpected historical_incidents in output when empty:\n%s", out)
	}
}

// TestFormatHypothesis_RendersSourceField verifies Source field renders in
// hypothesis when set.
func TestFormatHypothesis_RendersSourceField(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "handler.go:42 (hint match)", Source: "hint_match", AnomalyScore: 0.5},
		},
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, `source="hint_match"`) {
		t.Errorf("expected source attribute in hypothesis, got:\n%s", out)
	}
}
