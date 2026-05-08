package investigate

import (
	"sort"
	"time"
)

// ConfidenceLevel buckets a continuous score into a human-readable label.
type ConfidenceLevel string

const (
	ConfidenceLow    ConfidenceLevel = "low"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceHigh   ConfidenceLevel = "high"
)

// ConfidenceFromScore maps a [0, ∞) score to a 3-bucket confidence label.
//
//	score < 0.2  → low
//	0.2 ≤ x < 0.7 → medium
//	x ≥ 0.7      → high
func ConfidenceFromScore(score float64) ConfidenceLevel {
	switch {
	case score < 0.2:
		return ConfidenceLow
	case score < 0.7:
		return ConfidenceMedium
	default:
		return ConfidenceHigh
	}
}

// Hypothesis is one candidate root-cause site.
type Hypothesis struct {
	Subject string `json:"subject"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`

	SpanCount    int     `json:"span_count"`
	AnomalyScore float64 `json:"anomaly_score"`

	Confidence ConfidenceLevel `json:"confidence"`

	EvidenceLinks []string `json:"evidence_links,omitempty"`
	NextChecks    []string `json:"next_checks,omitempty"`
}

// InvestigationResult is the final tool output.
type InvestigationResult struct {
	Service    string       `json:"service"`
	Range      TimeRange    `json:"range"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Hypotheses []Hypothesis `json:"hypotheses"`
	LLMSummary string       `json:"llm_summary,omitempty"`
	Diagnostics Diagnostics `json:"diagnostics"`
}

// TimeRange is the [Start, End] window the investigation covered.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Diagnostics records counters from the investigation run for transparency.
type Diagnostics struct {
	MetricsQueried int      `json:"metrics_queried"`
	TracesFetched  int      `json:"traces_fetched"`
	SpansAnalyzed  int      `json:"spans_analyzed"`
	SymbolsTouched int      `json:"symbols_touched"`
	Warnings       []string `json:"warnings,omitempty"`
}

// compositeLess orders hypotheses by (span_count*anomaly) descending, stable.
func compositeLess(h []Hypothesis) func(i, j int) bool {
	return func(i, j int) bool {
		si := float64(h[i].SpanCount) * h[i].AnomalyScore
		sj := float64(h[j].SpanCount) * h[j].AnomalyScore
		return si > sj
	}
}

// RankHypotheses returns a copy of h sorted by composite score descending.
// Stable — equal scores preserve input order. Confidence label is recomputed
// from composite score / 10 (heuristic normalisation).
func RankHypotheses(h []Hypothesis) []Hypothesis {
	out := make([]Hypothesis, len(h))
	copy(out, h)
	sort.SliceStable(out, compositeLess(out))
	for i := range out {
		out[i].Confidence = ConfidenceFromScore(float64(out[i].SpanCount) * out[i].AnomalyScore / 10.0)
	}
	return out
}
