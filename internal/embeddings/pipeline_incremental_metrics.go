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

// embed_incremental_files_total counts files processed by Pipeline.IncrementalSync
// by change kind (embedded | skipped | deleted).
//
// Cardinality: 3 series.
var incrementalFilesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_files_total",
		Help: "Files processed by Pipeline.IncrementalSync by change kind.",
	},
	[]string{"kind"},
)

// embed_index_file_duration_seconds measures Pipeline.IndexFile wall-time per
// invocation, labelled by outcome (success | error | skipped).
//
// Buckets cover the observed range from 10ms (cache hit) to ~40s (large file embed).
// Cardinality: 3 series.
var indexFileDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "embed_index_file_duration_seconds",
		Help:    "Pipeline.IndexFile wall-time per file.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms → ~40s
	},
	[]string{"outcome"},
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
	for _, outcome := range []string{"success", "error", "skipped"} {
		indexFileDuration.WithLabelValues(outcome)
	}
}
