package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRecordHealthBuildFailure_CtxTimeout asserts that a context-deadline error
// increments the ctx_timeout series and does not touch compute_error.
func TestRecordHealthBuildFailure_CtxTimeout(t *testing.T) {
	ctxBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout))
	compBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError))

	recordHealthBuildFailure(context.DeadlineExceeded)

	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout)); got != ctxBefore+1 {
		t.Errorf("ctx_timeout counter = %.0f, want %.0f", got, ctxBefore+1)
	}
	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError)); got != compBefore {
		t.Errorf("compute_error counter moved unexpectedly: %.0f != %.0f", got, compBefore)
	}
}

// TestRecordHealthBuildFailure_ComputeError asserts that a non-context error
// increments compute_error and does not touch ctx_timeout.
func TestRecordHealthBuildFailure_ComputeError(t *testing.T) {
	ctxBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout))
	compBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError))

	recordHealthBuildFailure(errors.New("snapshot: unexpected EOF"))

	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError)); got != compBefore+1 {
		t.Errorf("compute_error counter = %.0f, want %.0f", got, compBefore+1)
	}
	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout)); got != ctxBefore {
		t.Errorf("ctx_timeout counter moved unexpectedly: %.0f != %.0f", got, ctxBefore)
	}
}

// TestRecordHealthBuildFailure_Nil asserts that a nil error (success path) does
// not increment any counter -- guards against a false alarm on healthy builds.
func TestRecordHealthBuildFailure_Nil(t *testing.T) {
	ctxBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout))
	compBefore := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError))

	recordHealthBuildFailure(nil)

	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout)); got != ctxBefore {
		t.Errorf("ctx_timeout counter moved on nil error: %.0f != %.0f", got, ctxBefore)
	}
	if got := testutil.ToFloat64(healthBuildFailures.WithLabelValues(healthBuildReasonComputeError)); got != compBefore {
		t.Errorf("compute_error counter moved on nil error: %.0f != %.0f", got, compBefore)
	}
}

// TestRecordCodeGraphBuildFailure_CtxTimeout asserts that a context-cancelled
// error increments the ctx_timeout series and does not touch index_error.
func TestRecordCodeGraphBuildFailure_CtxTimeout(t *testing.T) {
	ctxBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout))
	idxBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError))

	recordCodeGraphBuildFailure(context.Canceled)

	if got := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout)); got != ctxBefore+1 {
		t.Errorf("ctx_timeout counter = %.0f, want %.0f", got, ctxBefore+1)
	}
	if got := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError)); got != idxBefore {
		t.Errorf("index_error counter moved unexpectedly: %.0f != %.0f", got, idxBefore)
	}
}

// TestRecordCodeGraphBuildFailure_IndexError asserts that a non-context error
// increments index_error and does not touch ctx_timeout.
func TestRecordCodeGraphBuildFailure_IndexError(t *testing.T) {
	ctxBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout))
	idxBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError))

	recordCodeGraphBuildFailure(errors.New("pool: connection refused"))

	if got := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError)); got != idxBefore+1 {
		t.Errorf("index_error counter = %.0f, want %.0f", got, idxBefore+1)
	}
	if got := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout)); got != ctxBefore {
		t.Errorf("ctx_timeout counter moved unexpectedly: %.0f != %.0f", got, ctxBefore)
	}
}

// TestRecordCodeGraphAge_Set asserts that recordCodeGraphAge sets the gauge to a
// non-negative value (age >= 0 since builtAt is in the past or now).
func TestRecordCodeGraphAge_Set(t *testing.T) {
	repoKey := "test/age-gauge-repo"
	// builtAt one minute ago: age should be >= 60s.
	builtAt := time.Now().Add(-time.Minute)
	recordCodeGraphAge(repoKey, builtAt)

	got := testutil.ToFloat64(codeGraphAgeSeconds.WithLabelValues(repoKey))
	if got < 60 {
		t.Errorf("codeGraphAgeSeconds{repo=%q} = %.1f, want >= 60 (builtAt was 1 minute ago)", repoKey, got)
	}
}

// TestRecordCodeGraphAge_ZeroAge asserts that a builtAt of now() yields a very
// small age (< 5s), confirming the gauge tracks wall-clock time accurately.
func TestRecordCodeGraphAge_ZeroAge(t *testing.T) {
	repoKey := "test/age-gauge-zero"
	recordCodeGraphAge(repoKey, time.Now())

	got := testutil.ToFloat64(codeGraphAgeSeconds.WithLabelValues(repoKey))
	if got >= 5 {
		t.Errorf("codeGraphAgeSeconds{repo=%q} = %.1f, want < 5 (builtAt was now)", repoKey, got)
	}
}

// countGaugeSamples returns the number of distinct label-combination samples
// currently exported for the named metric family. Used to detect whether a
// call fabricated a new series, independent of any single series's value.
func countGaugeSamples(t *testing.T, name string) int {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return len(mf.GetMetric())
		}
	}
	return 0
}

// TestPublishCodeGraphAgeGauge_UnreachableStore_DoesNotFakeData is the
// regression guard for the "never fake freshness" requirement in the
// 2026-07-01 boot-warm fix: when the store is unreachable (DB outage at
// boot, or between ticker runs), publishCodeGraphAgeGauge must not
// fabricate ANY gocode_code_graph_age_seconds series — neither a seeded-0
// "looks fresh" value nor a sentinel "unknown repo" label. It must simply
// leave the gauge untouched and log a warning.
//
// RED before the fix: a "helpful" fallback (e.g. seeding a sentinel label
// on ListMeta error instead of returning early) would add a new sample to
// the family, and the sample-count-unchanged assertion fails.
func TestPublishCodeGraphAgeGauge_UnreachableStore_DoesNotFakeData(t *testing.T) {
	// Port 1 is not listening — pgxpool.Acquire (and therefore ListMeta's
	// acquireAGE) fails fast without a live network round-trip.
	cfg, err := pgxpool.ParseConfig("postgres://testuser:testpass@localhost:1/nodb?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()
	store := codegraph.NewStore(pool)

	const family = "gocode_code_graph_age_seconds"
	before := countGaugeSamples(t, family)

	publishCodeGraphAgeGauge(context.Background(), store)

	after := countGaugeSamples(t, family)
	if after != before {
		t.Errorf("publishCodeGraphAgeGauge with an unreachable store must not fabricate any "+
			"%s series: sample count %d -> %d", family, before, after)
	}
}
