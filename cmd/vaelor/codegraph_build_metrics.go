package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
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
// scopeDirs restricts publication to repos tracked under AUTO_INDEX_DIRS
// (see filterMetasByAutoIndexDirs) — code_graph_meta also holds rows for
// one-shot WORKSPACE_DIR clones and test sentinels that are never re-queried
// under AUTO_INDEX_DIRS, so their graphs are permanently "stale" by
// construction and would otherwise generate permanent false
// GocodeCodeGraphStale noise (confirmed live 2026-07-01: 7 of 28
// code_graph_meta rows were /tmp/go-code-workspace/* one-shot clones or a
// /test/skip/path sentinel). An empty scopeDirs (AUTO_INDEX_DIRS unset, as
// in dev/test) preserves the prior behavior of publishing every row.
//
// Repos with no code_graph_meta row (never built) are left unset. Do NOT
// seed 0 for them — an absent series correctly reads as "no data yet", and
// a fake 0 would misreport a never-built repo as freshly built, hiding real
// never-built state from GocodeCodeGraphStale (which alerts on staleness,
// not absence). If the store is unreachable, ListMeta returns an error and
// this call is a no-op — never fake freshness on a DB outage either.
func publishCodeGraphAgeGauge(ctx context.Context, store *codegraph.Store, scopeDirs []string) {
	metas, err := codegraph.ListMeta(ctx, store)
	if err != nil {
		slog.Warn("code_graph: age gauge warm failed", slog.Any("error", err))
		return
	}
	recordCodeGraphAges(metas, scopeDirs)
}

// recordCodeGraphAges sets the age gauge for every meta row in scope of
// scopeDirs (see filterMetasByAutoIndexDirs). Split out from
// publishCodeGraphAgeGauge so the scoping behavior is testable against a
// synthetic []codegraph.GraphMeta slice without a live DB connection.
func recordCodeGraphAges(metas []codegraph.GraphMeta, scopeDirs []string) {
	for _, m := range filterMetasByAutoIndexDirs(metas, scopeDirs) {
		recordCodeGraphAge(m.RepoKey, m.BuiltAt)
	}
}

// filterMetasByAutoIndexDirs returns the subset of metas whose RepoPath is
// in scope — equal to, or a subdirectory of, one of scopeDirs. When
// scopeDirs is empty (AUTO_INDEX_DIRS unset), all metas are returned
// unchanged: refusing to publish anything in that case would silently break
// the gauge in dev/test environments that never set AUTO_INDEX_DIRS.
func filterMetasByAutoIndexDirs(metas []codegraph.GraphMeta, scopeDirs []string) []codegraph.GraphMeta {
	if len(scopeDirs) == 0 {
		return metas
	}
	filtered := make([]codegraph.GraphMeta, 0, len(metas))
	for _, m := range metas {
		if repoPathInScope(m.RepoPath, scopeDirs) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// repoPathInScope reports whether repoPath equals, or is a subdirectory of,
// any entry in dirs. Boundary-safe: comparison is done on filepath.Clean'd
// paths with an explicit separator check, so "/host/src" matches
// "/host/src/go-nerv" but NOT "/host/src-other" (a raw strings.HasPrefix or
// strings.Contains would falsely match the latter).
func repoPathInScope(repoPath string, dirs []string) bool {
	clean := filepath.Clean(repoPath)
	for _, dir := range dirs {
		cleanDir := filepath.Clean(dir)
		if clean == cleanDir || strings.HasPrefix(clean, cleanDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
