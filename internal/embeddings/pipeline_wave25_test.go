package embeddings

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndexRepo_BumpsTimestampOnSameSHA: when IndexRepo is called twice with no
// changes between calls, the second call must update indexed_at even though no
// symbols are re-embedded (same-SHA short-circuit).
//
// Guards Item 3: bulk indexRepo same-SHA branch missing SetRepoState call.
// Mirrors TestIncrementalSync_BumpsTimestampOnSameSHA from Wave 2.
func TestIndexRepo_BumpsTimestampOnSameSHA(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/bulkpath-samesha-ts"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"ts_bulk.go": goFile("TimestampBulkFunc"),
	})

	// First call — bootstrap; sets indexed_at initially.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	tsBefore := rawGetIndexedAt(t, store, repo)

	// Sleep past Postgres TIMESTAMPTZ resolution (same reasoning as incremental test).
	time.Sleep(110 * time.Millisecond)

	// Second call — SHA identical, no symbol changes. indexed_at must advance.
	_, err = p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	tsAfter := rawGetIndexedAt(t, store, repo)
	assert.True(t, tsAfter.After(tsBefore),
		"IndexRepo same-SHA second call must bump indexed_at (tsBefore=%v tsAfter=%v)", tsBefore, tsAfter)
}

// TestIncrementalSyncMetrics_PreTouched: all 15 mode×outcome counter combinations
// must exist at startup (returning 0.0, not NaN or missing).
//
// Guards Item 5: non-pre-touched counters give no-data on fresh deploy
// (observability-gaps.md recurring family).
func TestIncrementalSyncMetrics_PreTouched(t *testing.T) {
	// Gather current metrics from the default registry.
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	// Build a lookup: metric_name -> label_values -> value.
	type labelSet struct{ mode, outcome string }
	found := make(map[labelSet]bool)
	for _, mf := range mfs {
		if mf.GetName() != "embed_incremental_sync_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var mode, outcome string
			for _, lp := range m.GetLabel() {
				switch lp.GetName() {
				case "mode":
					mode = lp.GetValue()
				case "outcome":
					outcome = lp.GetValue()
				}
			}
			found[labelSet{mode, outcome}] = true
		}
	}

	modes := []IncrementalSyncMode{
		IncrementalSyncIncremental,
		IncrementalSyncSkipSHAMatch,
		IncrementalSyncFullFallbackBootstrap,
		IncrementalSyncFullFallbackNoGit,
		IncrementalSyncFullFallbackDiffError,
	}
	outcomes := []string{"success", "partial", "error"}

	for _, mode := range modes {
		for _, outcome := range outcomes {
			ls := labelSet{string(mode), outcome}
			assert.True(t, found[ls],
				"embed_incremental_sync_total must be pre-touched for mode=%q outcome=%q (no-data on fresh deploy guard)",
				mode, outcome)
		}
	}
}

// TestIncrementalSyncMetrics_ModeEnum: all 5 IncrementalSyncMode constants must
// hold the exact string values required by Prometheus label consumers.
//
// Guards Item 2: typed enum values match the string literals used in code + dashboards.
func TestIncrementalSyncMetrics_ModeEnum(t *testing.T) {
	tests := []struct {
		mode IncrementalSyncMode
		want string
	}{
		{IncrementalSyncIncremental, "incremental"},
		{IncrementalSyncSkipSHAMatch, "skip-sha-match"},
		{IncrementalSyncFullFallbackBootstrap, "full-fallback-bootstrap"},
		{IncrementalSyncFullFallbackNoGit, "full-fallback-no-git"},
		{IncrementalSyncFullFallbackDiffError, "full-fallback-diff-error"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, string(tt.mode),
			"IncrementalSyncMode const value must match wire string")
	}
}

// TestIncrementalSyncMetrics_CounterIncremented: running IncrementalSync must
// increment embed_incremental_sync_total for the correct mode+outcome labels.
//
// Guards Item 5: counter is actually wired, not just declared.
func TestIncrementalSyncMetrics_CounterIncremented(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-metrics-counter"
	cleanRepoFull(t, store, repo)

	// Helper: read current counter value.
	readCounter := func(mode IncrementalSyncMode, outcome string) float64 {
		mfs, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		for _, mf := range mfs {
			if mf.GetName() != "embed_incremental_sync_total" {
				continue
			}
			for _, m := range mf.GetMetric() {
				var gotMode, gotOutcome string
				for _, lp := range m.GetLabel() {
					switch lp.GetName() {
					case "mode":
						gotMode = lp.GetValue()
					case "outcome":
						gotOutcome = lp.GetValue()
					}
				}
				if gotMode == string(mode) && gotOutcome == outcome {
					return m.GetCounter().GetValue()
				}
			}
		}
		return 0
	}

	root := initGitRepo(t, map[string]string{
		"counter_test_file.go": goFile("CounterFunc"),
	})

	// Snapshot before.
	beforeBootstrap := readCounter(IncrementalSyncFullFallbackBootstrap, "success")

	// Bootstrap call → full-fallback-bootstrap success.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.Equal(t, IncrementalSyncFullFallbackBootstrap, result.Mode)

	afterBootstrap := readCounter(IncrementalSyncFullFallbackBootstrap, "success")
	assert.Equal(t, beforeBootstrap+1, afterBootstrap,
		"embed_incremental_sync_total{mode=full-fallback-bootstrap,outcome=success} must increment by 1")

	// Snapshot before same-SHA.
	beforeSkip := readCounter(IncrementalSyncSkipSHAMatch, "success")

	// Same-SHA call → skip-sha-match success.
	result2, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.Equal(t, IncrementalSyncSkipSHAMatch, result2.Mode)

	afterSkip := readCounter(IncrementalSyncSkipSHAMatch, "success")
	assert.Equal(t, beforeSkip+1, afterSkip,
		"embed_incremental_sync_total{mode=skip-sha-match,outcome=success} must increment by 1")
}
