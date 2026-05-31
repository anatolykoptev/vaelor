package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// embed_incremental_sync_total counts Pipeline.IncrementalSync invocations by
// mode (the IncrementalSyncMode code path taken) and outcome.
//
// outcome values: success | partial | error
//   - success: no per-file errors, SHA advanced (or same-SHA skip)
//   - partial: at least one per-file error; SHA NOT advanced
//   - error:   catastrophic top-level error returned to caller
//
// Cardinality: 5 modes × 3 outcomes = 15 series max.
var incrementalSyncTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_sync_total",
		Help: "Pipeline.IncrementalSync invocations by mode and outcome.",
	},
	[]string{"mode", "outcome"},
)

// embed_incremental_files_total counts files processed by Pipeline.IndexFile
// by change kind (embedded | skipped | deleted).
//
// Counter increments BEFORE the SHA-advance gate, so partial-success runs
// (where some later file fails) are included; only top-level errors that
// abort before the per-file loop are excluded.
//
// Cardinality: 3 series.
var incrementalFilesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_files_total",
		Help: "Files processed by Pipeline.IndexFile by change kind. Includes files from partial-success runs (where some other file later failed); only excludes files where the parent IncrementalSync hit a top-level error before reaching the per-file loop.",
	},
	[]string{"kind"},
)

// embed_index_file_duration_seconds measures Pipeline.IndexFile wall-time per
// invocation, labelled by outcome (success | error).
//
// Buckets cover the observed range from 10ms (cache hit) to ~40s (large file embed).
// Cardinality: 2 series.
var indexFileDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "embed_index_file_duration_seconds",
		Help:    "Pipeline.IndexFile wall-time per file by outcome (success | error).",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms → ~40s
	},
	[]string{"outcome"},
)

// embed_incremental_unsupported_files_total counts files in incremental diffs
// that were permanently skipped due to a non-source-code reason, labelled by
// reason:
//
//   - unsupported_ext: extension has no tree-sitter handler (e.g. .md, .yml)
//   - read_error:      permanent IO error (permission denied, stale mount, etc)
//
// A non-zero rate of "unsupported_ext" is expected and benign (documentation
// commits). A non-zero rate of "read_error" warrants operator investigation.
//
// Cardinality: 2 series.
var incrementalFilesUnsupportedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_unsupported_files_total",
		Help: "Files in incremental diffs permanently skipped by reason (unsupported_ext | read_error).",
	},
	[]string{"reason"},
)

// gocode_index_freshness_lag is a per-repo gauge that records whether the repo's
// indexed_sha is current after the last IncrementalSync run.
//
//   - 0: indexed_sha == mainSHA (fully up-to-date)
//   - 1: indexed_sha != mainSHA after the run (lag detected)
//
// A persistent 1 for a repo indicates that repeated sync failures are preventing
// SHA advance — the classic symptom of the unsupported-file freeze bug or a
// permanent embed-server error for that repo.
//
// Label "repo" uses the repoKey (e.g. "github.com/org/repo"). Cardinality is
// bounded by the number of indexed repos (typically 10-100).
var indexFreshnessLag = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_freshness_lag",
		Help: "1 if indexed_sha != mainSHA after IncrementalSync, 0 if up-to-date.",
	},
	[]string{"repo"},
)

// recordIncrementalSync increments embed_incremental_sync_total for the given
// result. Called at every return point of IncrementalSync.
// When err is non-nil (catastrophic failure), outcome = "error".
// When result.Errors is non-empty, outcome = "partial".
// Otherwise outcome = "success".
func recordIncrementalSync(result *IncrementalSyncResult, err error) {
	if result == nil {
		return
	}
	outcome := "success"
	switch {
	case err != nil:
		outcome = "error"
	case len(result.Errors) > 0:
		outcome = "partial"
	}
	incrementalSyncTotal.WithLabelValues(string(result.Mode), outcome).Inc()
}

func init() {
	// Pre-touch all counter label combinations so Prometheus exposes the series
	// immediately on startup (before any IncrementalSync call). Without this,
	// fresh-deploy dashboards show "no data" instead of 0.
	// See: observability-gaps.md "non-pre-touched counters" family.
	allModes := []IncrementalSyncMode{
		IncrementalSyncIncremental,
		IncrementalSyncSkipSHAMatch,
		IncrementalSyncFullFallbackBootstrap,
		IncrementalSyncFullFallbackNoGit,
		IncrementalSyncFullFallbackDiffError,
	}
	for _, mode := range allModes {
		for _, outcome := range []string{"success", "partial", "error"} {
			incrementalSyncTotal.WithLabelValues(string(mode), outcome)
		}
	}
	for _, kind := range []string{"embedded", "skipped", "deleted"} {
		incrementalFilesTotal.WithLabelValues(kind)
	}
	for _, outcome := range []string{"success", "error"} {
		indexFileDuration.WithLabelValues(outcome)
	}
	for _, reason := range []string{"unsupported_ext", "read_error"} {
		incrementalFilesUnsupportedTotal.WithLabelValues(reason)
	}
}
