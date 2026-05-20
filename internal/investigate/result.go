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
	// EndLine is the last line of the function body (1-based, inclusive).
	// Populated from parser.Symbol.EndLine in Tier-3 callgraph path.
	// For Tier-1 (OTEL code.* tags) it equals Line (single attribution point).
	EndLine int `json:"end_line,omitempty"`

	SpanCount    int     `json:"span_count"`
	AnomalyScore float64 `json:"anomaly_score"`

	// Confidence is a bucketed label (low/medium/high). Populated by
	// RankHypotheses ONLY if caller left it empty — caller-set values
	// (typically by the LLM correlate step which weighs evidence holistically)
	// are preserved. To force the heuristic, leave this zero on input.
	Confidence ConfidenceLevel `json:"confidence"`

	Impact     *ImpactInfo     `json:"impact,omitempty"`
	SymbolBody *SymbolBodyInfo `json:"symbol_body,omitempty"`

	EvidenceLinks []string    `json:"evidence_links,omitempty"`
	NextChecks    []NextCheck `json:"next_checks,omitempty"`

	// Source tracks the origin of this hypothesis: span | hint_match | alert | "" (backwards compat).
	Source string `json:"source,omitempty"`

	// FusedScore is the multi-signal combined score (Phase γ.D). Set after
	// all enrichment phases. Sortable. Higher = more likely root cause.
	FusedScore float64 `json:"fused_score,omitempty"`

	// SignalBreakdown shows individual normalized signal values that fed
	// into FusedScore. Useful for operator triage and debugging the rank.
	SignalBreakdown map[string]float64 `json:"signal_breakdown,omitempty"`

	// RecentChange embeds a recent git diff for the hypothesis file (Phase γ.D).
	// Set for top-1 hypothesis only when a repo path is available.
	RecentChange *RecentChange `json:"recent_change,omitempty"`

	// BodySource is the raw source text of the function body (Sprint B1).
	// Populated for top-3 hypotheses after FusionRank. Enables LLM to
	// reason about the code rather than just its metadata.
	// Empty when file read fails or File is empty (no symbol resolved).
	BodySource string `json:"body_source,omitempty"`
}

// RecentChange captures a recent git diff for a hypothesis file.
type RecentChange struct {
	File  string `json:"file"`
	Since string `json:"since"`          // e.g. "2026-04-09" — 30 days ago
	Diff  string `json:"diff,omitempty"` // unified diff, capped at maxLines
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
	Service             string               `json:"service"`
	Range               TimeRange            `json:"range"`
	StartedAt           time.Time            `json:"started_at"`
	FinishedAt          time.Time            `json:"finished_at"`
	Hypotheses          []Hypothesis         `json:"hypotheses"`
	LLMSummary          string               `json:"llm_summary,omitempty"`
	MetricSpikes        []MetricSpike        `json:"metric_spikes,omitempty"`
	AlertViolations     []AlertViolation     `json:"alert_violations,omitempty"`
	HintKind            string               `json:"hint_kind,omitempty"`
	LogExcerpts         []LogExcerpt         `json:"log_excerpts,omitempty"`
	HistoricalIncidents []HistoricalIncident `json:"historical_incidents,omitempty"`
	Diagnostics         Diagnostics          `json:"diagnostics"`

	// RuntimeVersions, when non-nil, carries the Phase 7 deployed-image
	// diff against pinned source. Nil = phase skipped or host not provided.
	RuntimeVersions *FleetReport `json:"runtime_versions,omitempty"`
}

// TimeRange is the [Start, End] window the investigation covered.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Diagnostics records counters from the investigation run for transparency.
type Diagnostics struct {
	MetricsQueried          int      `json:"metrics_queried"`
	TracesFetched           int      `json:"traces_fetched"`
	SpansAnalyzed           int      `json:"spans_analyzed"`
	SymbolsTouched          int      `json:"symbols_touched"`
	AlertsQueried           int      `json:"alerts_queried,omitempty"`
	LogsFetched             int      `json:"logs_fetched,omitempty"`
	HypothesesDroppedAsDead int      `json:"hypotheses_dropped_as_dead,omitempty"`
	LearningsPersisted      bool     `json:"learnings_persisted,omitempty"`
	LLMCacheHit             bool     `json:"llm_cache_hit,omitempty"`
	LLMSkippedReason        string   `json:"llm_skipped_reason,omitempty"`
	Warnings                []string `json:"warnings,omitempty"`
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

// BlastRadius enum values for ImpactInfo.BlastRadius.
const (
	BlastRadiusNone   = "none"
	BlastRadiusLow    = "low"
	BlastRadiusMedium = "medium"
	BlastRadiusHigh   = "high"
)

// ImpactInfo captures blast-radius data from impact.Analyze for a hypothesis.
type ImpactInfo struct {
	DirectCallers int     `json:"direct_callers"`
	TotalAffected int     `json:"total_affected"`
	BlastRadius   string  `json:"blast_radius,omitempty"` // BlastRadiusNone | BlastRadiusLow | BlastRadiusMedium | BlastRadiusHigh
	RiskScore     float64 `json:"risk_score,omitempty"`
}

// SymbolBodyInfo captures structural analysis from compound.AnalyzeBody for a hypothesis.
type SymbolBodyInfo struct {
	ErrorExits      int  `json:"error_exits"`
	HasDeferCleanup bool `json:"has_defer_cleanup,omitempty"`
	HasTODO         bool `json:"has_todo,omitempty"`
}

// Source values for Hypothesis.Source (γ.C.3).
const (
	HypothesisSourceSpan      = "span"
	HypothesisSourceHintMatch = "hint_match"
	HypothesisSourceAlert     = "alert"
	// HypothesisSourceUpstream marks a hypothesis generated by the upstream
	// callgraph walk (Sprint B2). These hypotheses are callers of a Tier-3
	// span-attributed hypothesis and represent root-cause candidates upstream
	// of the observed metric attribution site.
	HypothesisSourceUpstream = "upstream_caller"
	// HypothesisSourceDownstream marks a hypothesis generated by the downstream
	// callgraph walk (Sprint B4). These hypotheses are callees of the top-1
	// span-attributed hypothesis and represent root-cause candidates downstream
	// of the observed metric attribution site (e.g. an event-loop dispatcher
	// that calls the handler that actually contains the bug).
	HypothesisSourceDownstream = "downstream_callee"
)

// NextCheck is a structured follow-up recommendation for the operator or agent.
// It names the tool to call and the arguments to pass.
type NextCheck struct {
	Tool string            `json:"tool"`           // Tool must be non-empty (the MCP tool name to invoke). e.g. "understand", "code_health"
	Args map[string]string `json:"args,omitempty"` // named arguments for the tool
}

// HistoricalIncident is a past investigation record retrieved from the learnings store.
type HistoricalIncident struct {
	Repo      string `json:"repo"`
	Symbol    string `json:"symbol"`
	RiskLevel string `json:"risk_level,omitempty"`
	Flag      string `json:"flag,omitempty"`
	Note      string `json:"note,omitempty"`
}

// OperationInfo aggregates per-operation context from a service's spans.
// Used by Phase 3 (runSymbolsPhase) to feed symbol resolution with both span
// counts and OTEL semantic-convention attributes. When code.* tags are present,
// Phase 3 resolves directly to file:line without needing a callgraph.
type OperationInfo struct {
	Operation     string // span operation name (key)
	Count         int    // CUMULATIVE — total spans for this op in window
	HTTPRoute     string // FIRST-SEEN — http.route tag (axum MatchedPath)
	HTTPMethod    string // FIRST-SEEN — http.method tag
	CodeFilepath  string // FIRST-SEEN — code.filepath OR code.file.path (absolute path inside container)
	CodeLineno    int    // FIRST-SEEN — code.lineno OR code.line.number
	CodeNamespace string // FIRST-SEEN — code.namespace OR code.module.name (e.g. Rust module path)
	CodeFunction  string // FIRST-SEEN — code.function OR code.function.name (e.g. Go method like (*T).Method)
}
