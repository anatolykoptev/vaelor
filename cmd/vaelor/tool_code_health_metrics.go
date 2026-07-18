package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Drop-reason label values for healthSnapshotFilesDropped. Each names the
// OBSERVED condition at the emit site (metrics-convention C1), not an inferred
// cause: read_error is "os.ReadFile failed on an enumerated file"; ctx_cancel
// is "parse worker returned early on ctx.Err()". They are split (C2) so a
// PromQL alert can fire on the use-after-delete population (read_error) without
// being diluted by legitimate timeout cancellations (ctx_cancel).
const (
	healthDropReasonReadError = "read_error"
	healthDropReasonCtxCancel = "ctx_cancel"
)

// healthSnapshotFilesDropped counts source files that code_health enumerated
// but could not fold into the snapshot.
//
//   - reason: read_error | ctx_cancel
//
// A nonzero {reason="read_error"} rate is the smoking gun for a clone deleted
// out from under a live snapshot walk (the bug this fix closes). It stays at a
// flat zero baseline in healthy operation, so any sustained increase is
// actionable. Cardinality: 2 series.
var healthSnapshotFilesDropped = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_codehealth_snapshot_files_dropped_total",
		Help: "Source files dropped from a code_health snapshot, by observed reason (read_error, ctx_cancel).",
	},
	[]string{"reason"},
)

// healthCloneMissing counts code_health computations whose snapshot retained
// far fewer files than ingest enumerated — i.e. the clone tree was (partially)
// missing while the snapshot was built. Distinct from the per-file drop counter:
// this fires once per AFFECTED computation, so it answers "how many reports were
// degraded" rather than "how many files vanished". A flat zero is the healthy
// baseline; any increment means a report was served (and cached) on a
// truncated tree.
var healthCloneMissing = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_codehealth_clone_missing_total",
		Help: "code_health computations where the snapshot file count fell materially below the enumerated count (clone vanished mid-build).",
	},
)

// Pre-touch every series at zero so /metrics exposes them from a cold start —
// otherwise an alert on these counters has no baseline until the first drop.
// Matches the repo convention (resolve_metrics.go, tool_semantic_search_hybrid.go).
func init() {
	healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError).Add(0)
	healthSnapshotFilesDropped.WithLabelValues(healthDropReasonCtxCancel).Add(0)
	healthCloneMissing.Add(0)
}

// recordSnapshotDrops bumps the drop counters from a snapshot's per-reason drop
// tallies and fires the clone-missing counter once when the surviving file
// count fell materially below the enumerated count. cloneMissingThreshold is
// the fraction of enumerated files that must survive for the result to be
// considered complete; below it, the result is treated as a vanished-clone
// degradation.
func recordSnapshotDrops(droppedReadError, droppedCtxCancel, fileCount, survivingFiles int) {
	if droppedReadError > 0 {
		healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError).Add(float64(droppedReadError))
	}
	if droppedCtxCancel > 0 {
		healthSnapshotFilesDropped.WithLabelValues(healthDropReasonCtxCancel).Add(float64(droppedCtxCancel))
	}
	// Clone-missing: enumerated N files but kept < threshold·N. Guard fileCount>0
	// so an empty repo (private-clone-no-token) does not trip the alarm.
	if fileCount > 0 && float64(survivingFiles) < cloneMissingThreshold*float64(fileCount) {
		healthCloneMissing.Inc()
	}
}

// cloneMissingThreshold: a snapshot keeping fewer than 50% of the enumerated
// files is treated as a vanished-clone degradation. A normal snapshot keeps
// ~100% (only unreadable/oversize files drop), so the gap to 50% is wide enough
// to avoid false positives from a handful of legitimately-skipped files.
const cloneMissingThreshold = 0.5
