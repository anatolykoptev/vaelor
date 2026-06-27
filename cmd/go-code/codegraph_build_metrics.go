package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Reason label values for codeGraphBuildFailures. Each names the observed
// failure class at the emit site:
//
//   - "ctx_timeout"  -- background IndexRepo context deadline exceeded.
//   - "index_error"  -- IndexRepo returned a non-context error.
const (
	codeGraphBuildReasonCtxTimeout = "ctx_timeout"
	codeGraphBuildReasonIndexError = "index_error"
)

// codeGraphBuildFailures counts background code_graph IndexRepo calls that
// returned an error, labelled by failure class.
//
// Pre-touched at 0 so /metrics exports both series from a cold start.
var codeGraphBuildFailures = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_code_graph_build_failures_total",
		Help: "Background code_graph IndexRepo builds that failed, by reason (ctx_timeout, index_error).",
	},
	[]string{"reason"},
)

// codeGraphAgeSeconds tracks graph staleness per repo. Set to
// time.Since(meta.BuiltAt).Seconds() after each successful IndexRepo.
// A stale graph (age > threshold) is now observable without reading tool text.
//
// Cardinality: one series per distinct repo key queried (same as existing
// gocode_index_embeddings_coverage_rows{repo} gauge).
var codeGraphAgeSeconds = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_code_graph_age_seconds",
		Help: "Seconds since the code_graph for a repo was last successfully built (staleness indicator).",
	},
	[]string{"repo"},
)

func init() {
	codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout).Add(0)
	codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError).Add(0)
}

// recordCodeGraphBuildFailure bumps the appropriate code-graph-build-failure
// counter. A context-deadline or cancellation error maps to ctx_timeout; all
// other errors map to index_error.
func recordCodeGraphBuildFailure(err error) {
	if err == nil {
		return
	}
	reason := codeGraphBuildReasonIndexError
	if isCtxError(err) {
		reason = codeGraphBuildReasonCtxTimeout
	}
	codeGraphBuildFailures.WithLabelValues(reason).Inc()
}

// recordCodeGraphAge sets the age gauge for repoKey after a successful build.
// builtAt is the meta.BuiltAt timestamp returned by IndexRepo.
func recordCodeGraphAge(repoKey string, builtAt time.Time) {
	codeGraphAgeSeconds.WithLabelValues(repoKey).Set(time.Since(builtAt).Seconds())
}
