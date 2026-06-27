package main

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Reason label values for healthBuildFailures. Each names the observed failure
// class at the emit site:
//
//   - "ctx_timeout"   -- background build context deadline exceeded.
//   - "compute_error" -- computeCodeHealth returned a non-context error.
const (
	healthBuildReasonCtxTimeout   = "ctx_timeout"
	healthBuildReasonComputeError = "compute_error"
)

// healthBuildFailures counts background code_health computations that did not
// complete successfully, labelled by the failure class.
//
// Pre-touched at 0 so /metrics always exports both series from a cold start.
var healthBuildFailures = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_codehealth_build_failures_total",
		Help: "Background code_health computations that failed, by reason (ctx_timeout, compute_error).",
	},
	[]string{"reason"},
)

func init() {
	healthBuildFailures.WithLabelValues(healthBuildReasonCtxTimeout).Add(0)
	healthBuildFailures.WithLabelValues(healthBuildReasonComputeError).Add(0)
}

// recordHealthBuildFailure bumps the appropriate health-build-failure counter.
// A context-deadline or cancellation error maps to ctx_timeout; all others to compute_error.
func recordHealthBuildFailure(err error) {
	if err == nil {
		return
	}
	reason := healthBuildReasonComputeError
	if isCtxError(err) {
		reason = healthBuildReasonCtxTimeout
	}
	healthBuildFailures.WithLabelValues(reason).Inc()
}

// isCtxError reports whether err is (or wraps) a context deadline or cancellation error.
func isCtxError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
