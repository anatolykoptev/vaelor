package investigate

import (
	"strconv"
	"time"

	"github.com/anatolykoptev/go-kit/rerank"
	"github.com/anatolykoptev/go-kit/score"
)

// ConfidenceLevel is re-exported from go-kit/score so callers of the
// investigate package don't need to import score directly. The underlying
// type is identical — investigate.ConfidenceLevel == score.ConfidenceLevel.
type ConfidenceLevel = score.ConfidenceLevel

// Re-exported confidence labels — identical to the score package consts.
const (
	ConfidenceLow    = score.ConfidenceLow
	ConfidenceMedium = score.ConfidenceMedium
	ConfidenceHigh   = score.ConfidenceHigh
)

// ConfidenceFromScore is re-exported as a thin wrapper around score.ConfidenceFromScore.
// Defaults: <0.2 → low, <0.7 → medium, ≥0.7 → high.
func ConfidenceFromScore(s float64) ConfidenceLevel {
	return score.ConfidenceFromScore(s)
}

// Hypothesis is one candidate root-cause site.
type Hypothesis struct {
	Subject string `json:"subject"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`

	SpanCount    int     `json:"span_count"`
	AnomalyScore float64 `json:"anomaly_score"`

	// Confidence is a bucketed label (low/medium/high). Populated by
	// RankHypotheses ONLY if caller left it empty — caller-set values
	// (typically by the LLM correlate step which weighs evidence holistically)
	// are preserved. To force the heuristic, leave this zero on input.
	Confidence ConfidenceLevel `json:"confidence"`

	EvidenceLinks []string `json:"evidence_links,omitempty"`
	NextChecks    []string `json:"next_checks,omitempty"`
}

// MetricSpike captures a single metric (failure / latency / saturation) showing anomaly above baseline.
type MetricSpike struct {
	Kind       string  `json:"kind"`        // failure | latency | saturation
	MetricName string  `json:"metric_name"` // full Prometheus metric name
	Labels     string  `json:"labels"`      // label-set rendered for human reading
	Ratio      float64 `json:"ratio"`       // window_max / baseline_max
	Score      float64 `json:"score"`       // bucketed anomaly score 0..1
}

// InvestigationResult is the final tool output.
//
// Time fields:
//   - StartedAt / FinishedAt: wall-clock — when this investigation
//     started and finished executing.
//   - Range: the *data* window the investigation analysed (Prometheus
//     query range, Jaeger trace search range). Independent of wall-clock.
type InvestigationResult struct {
	Service      string        `json:"service"`
	Range        TimeRange     `json:"range"`
	StartedAt    time.Time     `json:"started_at"`
	FinishedAt   time.Time     `json:"finished_at"`
	Hypotheses   []Hypothesis  `json:"hypotheses"`
	LLMSummary   string        `json:"llm_summary,omitempty"`
	MetricSpikes []MetricSpike `json:"metric_spikes,omitempty"`
	HintKind     string        `json:"hint_kind,omitempty"`
	Diagnostics  Diagnostics   `json:"diagnostics"`
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

// RankHypotheses returns a copy of h sorted by fused score (descending).
// Fusion: rerank.LinearMinMax with equal weights on SpanCount and
// AnomalyScore signals. Both signals are MinMax-normalised to [0,1] within
// the input set, summed (weights 1:1), resulting score is in [0,2].
//
// Confidence policy: caller-set values are PRESERVED; empty Confidence is
// filled via ConfidenceFromScore on the fused score. The LLM correlate step
// (Task 13) populates Confidence based on holistic evidence and survives.
//
// Stable: equal scores preserve input order (rerank.LinearMinMax guarantees).
//
// Empty input → nil. Single-element input → that element with Confidence
// filled from heuristic on fused=0 (degenerate; LLM step typically produces
// meaningful labels in real flows).
func RankHypotheses(h []Hypothesis) []Hypothesis {
	if len(h) == 0 {
		return nil
	}
	spanList := make(rerank.ScoredIDList, len(h))
	anomalyList := make(rerank.ScoredIDList, len(h))
	for i := range h {
		id := strconv.Itoa(i)
		spanList[i] = rerank.ScoredID{ID: id, Score: float64(h[i].SpanCount)}
		anomalyList[i] = rerank.ScoredID{ID: id, Score: h[i].AnomalyScore}
	}

	// Equal weights — both signals matter symmetrically. Future calibration
	// (e.g. weight=2.0 on anomaly when traces sparse) can be exposed as
	// RankHypothesesWithWeights without breaking this default API.
	fused := rerank.LinearMinMax([]float64{1.0, 1.0}, spanList, anomalyList)

	out := make([]Hypothesis, 0, len(h))
	for _, f := range fused {
		idx, err := strconv.Atoi(f.ID)
		if err != nil || idx < 0 || idx >= len(h) {
			continue // defensive — should not happen given how IDs are built
		}
		hyp := h[idx]
		if hyp.Confidence == "" {
			hyp.Confidence = ConfidenceFromScore(f.Score)
		}
		out = append(out, hyp)
	}
	return out
}
