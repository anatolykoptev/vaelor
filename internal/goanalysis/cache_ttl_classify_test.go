package goanalysis

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCacheTTLFor_ClassifiesCallerBudgetVsGenuineFailure is the deterministic
// half of the H2 fix from the go-code PR #294 pr-review-council report
// (reviews/pr-council/pr-294-2026-07-01.md): cacheTTLFor must never cache a
// caller-budget-specific failure (ctx done — deadline exceeded or
// canceled), while a genuine repo-level failure IS cached under
// loadCacheNegativeTTL and a success under loadCacheTTL.
//
// This is an internal (package goanalysis, not goanalysis_test) test so it
// can call cacheTTLFor directly with controlled ctx states, sidestepping
// golang.org/x/tools/go/packages' unreliable error-wrapping on a real
// packages.Load timeout (see TestCachedLoadPackages_DeadlineExceededNotCached
// in cached_loader_test.go for the integration-level proof of the same
// fix, and its doc comment for the empirical evidence that pattern-matching
// the error value — rather than checking ctx.Err() — would be unreliable).
// This mirrors internal/callgraph/metrics_test.go's identical workaround for
// the exact same underlying issue with isDeadlineErr/TryGoTypesResolution.
//
// RED guarantee: revert cacheTTLFor to ignore ctx and unconditionally
// return loadCacheNegativeTTL for any non-nil error (the pre-H2-fix
// behavior when driven only by err classification) and the "context done"
// subtests fail (expect 0, got loadCacheNegativeTTL).
func TestCacheTTLFor_ClassifiesCallerBudgetVsGenuineFailure(t *testing.T) {
	deadlineCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // guarantee the deadline has passed

	canceledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want time.Duration
	}{
		{"success", context.Background(), nil, loadCacheTTL},
		{"caller ctx deadline exceeded - never cache", deadlineCtx, deadlineCtx.Err(), 0},
		{"caller ctx canceled - never cache", canceledCtx, canceledCtx.Err(), 0},
		{"genuine failure, live ctx - negative TTL", context.Background(), errors.New("no go.mod found in /tmp/x"), loadCacheNegativeTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheTTLFor(tt.ctx, tt.err); got != tt.want {
				t.Errorf("cacheTTLFor(ctx, %v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
