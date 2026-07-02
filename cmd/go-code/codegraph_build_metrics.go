package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
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

// publishCodeGraphAgeGauge lists every known repo snapshot (code_graph_meta)
// and (re)sets gocode_code_graph_age_seconds{repo} from its real BuiltAt
// timestamp. Called at boot and on a periodic ticker (register.go, mirroring
// the existing 5-min orphan-gauge pattern) so the gauge:
//
//   - exists immediately after a restart instead of vanishing until the next
//     successful build. A stale graph is exactly the state
//     GocodeCodeGraphStale wants to catch, but an absent series evaluates to
//     no data — not ">93600" — so the alert went dark on every deploy
//     (confirmed live 2026-07-01: the v1.22.1 rollout dropped the series).
//   - keeps growing between builds instead of freezing at the near-zero
//     value recordCodeGraphAge set right after the last successful build:
//     without a periodic re-Set, a repo whose background rebuild silently
//     stops firing would never cross the staleness threshold because the
//     gauge is never touched again.
//
// Repos with no code_graph_meta row (never built) are left unset. Do NOT
// seed 0 for them — an absent series correctly reads as "no data yet", and
// a fake 0 would misreport a never-built repo as freshly built, hiding real
// never-built state from GocodeCodeGraphStale (which alerts on staleness,
// not absence). If the store is unreachable, ListMeta returns an error and
// this call is a no-op — never fake freshness on a DB outage either.
func publishCodeGraphAgeGauge(ctx context.Context, store *codegraph.Store) {
	metas, err := codegraph.ListMeta(ctx, store)
	if err != nil {
		slog.Warn("code_graph: age gauge warm failed", slog.Any("error", err))
		return
	}
	for _, m := range metas {
		recordCodeGraphAge(m.RepoKey, m.BuiltAt)
	}
}
