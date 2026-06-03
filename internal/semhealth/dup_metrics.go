package semhealth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Semantic-dup triage metrics — pre-touched at 0 in init() so /metrics always
// exposes them regardless of whether any triage has run (project metrics-first
// rule: no silent absences on write-failure or error paths).
//
// gocode_semhealth_dup_candidates_total{repo}   — raw similar-pair count before filters.
// gocode_semhealth_dup_reported_total{tier}     — groups surfaced per tier.
// gocode_semhealth_dup_filtered_total{filter}   — pairs dropped per filter step.
// gocode_semhealth_dup_errors_total{stage}      — query errors per triage stage.
// gocode_semhealth_dup_duration_seconds         — AnalyzeTriage wall-clock latency.
var (
	dupCandidatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_semhealth_dup_candidates_total",
			Help: "Raw similar-pair count returned by FindSimilarPairs before any filtering.",
		},
		[]string{"repo"},
	)
	dupReportedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_semhealth_dup_reported_total",
			Help: "Number of duplicate groups surfaced to the caller, by tier (exact, very-close, related).",
		},
		[]string{"tier"},
	)
	dupFilteredTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_semhealth_dup_filtered_total",
			Help: "Number of similar pairs dropped by each filter step.",
		},
		[]string{"filter"},
	)
	dupErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_semhealth_dup_errors_total",
			Help: "Query errors per triage stage (exact_query, similar_query).",
		},
		[]string{"stage"},
	)
	dupDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gocode_semhealth_dup_duration_seconds",
			Help:    "Wall-clock latency of AnalyzeTriage from first query to result assembly.",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func init() {
	// Pre-touch tier labels.
	for _, tier := range []string{
		dupTierExact,
		dupTierVeryClose,
		dupTierRelated,
	} {
		dupReportedTotal.WithLabelValues(tier).Add(0)
	}
	// Pre-touch filter labels.
	for _, filter := range []string{
		dupFilterTests,
		dupFilterSameFile,
		dupFilterKind,
		dupFilterCallsEdge,
		dupFilterInterfaceSibling,
	} {
		dupFilteredTotal.WithLabelValues(filter).Add(0)
	}
	// Pre-touch error stage labels.
	for _, stage := range []string{
		dupStageExactQuery,
		dupStageSimilarQuery,
	} {
		dupErrorsTotal.WithLabelValues(stage).Add(0)
	}
}
