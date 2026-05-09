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

// Score type conventions (γ.A.5 audit):
//
//   - ConfidenceLevel (Hypothesis.Confidence): uses go-kit/score.ConfidenceLevel —
//     a strong type with bucketed labels (low/medium/high). Use when displaying
//     or serialising confidence to humans or LLMs.
//
//   - Hypothesis.AnomalyScore float64: raw 0..1 multiplier. Fed directly into
//     rerank.ScoredID.Score (expects float64). Converting to score.Score would
//     require un-boxing at the rerank call site — no net benefit. Keep as float64.
//
//   - MetricSpike.Score float64: raw 0..1 bucket value (scoreCritical=1.0, etc.).
//     Used for ranking spikes within an investigation; not surfaced as a typed
//     confidence label. Keep as float64.
//
// Rule: use go-kit/score types for values shown to humans/LLMs (ConfidenceLevel).
// Keep float64 for raw numeric multipliers consumed internally by rerank or math.

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

// AlertViolation captures a firing Prometheus alert that matches the investigated service.
// These represent constant-state invariant violations that Phase 4 spike detection
// misses because there is no delta — the ratio never changes, it is just always wrong.
type AlertViolation struct {
	AlertName   string `json:"alert_name"`
	Severity    string `json:"severity"`
	Service     string `json:"service"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Runbook     string `json:"runbook,omitempty"`
	ActiveAt    string `json:"active_at,omitempty"`
}

// InvestigationResult is the final tool output.
//
// Time fields:
//   - StartedAt / FinishedAt: wall-clock — when this investigation
//     started and finished executing.
//   - Range: the *data* window the investigation analysed (Prometheus
//     query range, Jaeger trace search range). Independent of wall-clock.
type InvestigationResult struct {
	Service         string           `json:"service"`
	Range           TimeRange        `json:"range"`
	StartedAt       time.Time        `json:"started_at"`
	FinishedAt      time.Time        `json:"finished_at"`
	Hypotheses      []Hypothesis     `json:"hypotheses"`
	LLMSummary      string           `json:"llm_summary,omitempty"`
	MetricSpikes    []MetricSpike    `json:"metric_spikes,omitempty"`
	AlertViolations []AlertViolation `json:"alert_violations,omitempty"`
	HintKind        string           `json:"hint_kind,omitempty"`
	LogExcerpts     []LogExcerpt     `json:"log_excerpts,omitempty"`
	Diagnostics     Diagnostics      `json:"diagnostics"`
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
	AlertsQueried  int      `json:"alerts_queried,omitempty"`
	LogsFetched    int      `json:"logs_fetched,omitempty"`
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

// LogExcerpt is a single log line from the dozor sidecar, attached to the
// investigation result when dozor is configured.
type LogExcerpt struct {
	Ts    string `json:"ts,omitempty"`
	Level string `json:"level,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Raw   string `json:"raw,omitempty"`
}
