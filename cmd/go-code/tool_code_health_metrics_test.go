package main

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRecordSnapshotDrops_Counters proves the drop counters move by reason and
// that clone-missing fires only when the surviving file count collapses below
// the threshold — the observability that makes a future race visible instead of
// silently wrong.
func TestRecordSnapshotDrops_Counters(t *testing.T) {
	readBefore := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError))
	ctxBefore := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonCtxCancel))
	missBefore := testutil.ToFloat64(healthCloneMissing)

	// 8 read drops + 3 ctx drops; 2 of 155 files survived → clone-missing fires.
	recordSnapshotDrops(8, 3, 155, 2)

	if got := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError)); got != readBefore+8 {
		t.Errorf("read_error counter = %v, want %v", got, readBefore+8)
	}
	if got := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonCtxCancel)); got != ctxBefore+3 {
		t.Errorf("ctx_cancel counter = %v, want %v", got, ctxBefore+3)
	}
	if got := testutil.ToFloat64(healthCloneMissing); got != missBefore+1 {
		t.Errorf("clone_missing counter = %v, want %v", got, missBefore+1)
	}
}

// TestRecordSnapshotDrops_CompleteNoFire proves a complete snapshot (no drops,
// all files survived) does not move any counter — guards against a false-alarm
// clone-missing on healthy reports and against an empty-repo trip.
func TestRecordSnapshotDrops_CompleteNoFire(t *testing.T) {
	readBefore := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError))
	missBefore := testutil.ToFloat64(healthCloneMissing)

	recordSnapshotDrops(0, 0, 155, 155) // all files survived
	recordSnapshotDrops(0, 0, 0, 0)     // empty repo (e.g. private clone, no token)

	if got := testutil.ToFloat64(healthSnapshotFilesDropped.WithLabelValues(healthDropReasonReadError)); got != readBefore {
		t.Errorf("read_error counter moved on a complete snapshot: %v != %v", got, readBefore)
	}
	if got := testutil.ToFloat64(healthCloneMissing); got != missBefore {
		t.Errorf("clone_missing fired on a complete/empty snapshot: %v != %v", got, missBefore)
	}
}
