package main

import (
	"context"
	"errors"
	"time"

	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	kitllm "github.com/anatolykoptev/go-kit/llm"
)

// llmObs holds LLM call metrics registered against the application registry.
// Constructed once in main via newLLMObs and threaded through registerTools.
//
// Counter: gocode_llm_calls_total{outcome=...}
// Histogram: gocode_llm_request_seconds{outcome=...}
//
// Both route through the kitmetrics bridge so they pick up the registry
// namespace ("gocode") automatically and appear consistently on /metrics.
// No direct prometheus dependency needed.
//
// Bucket note: the bridge auto-registers with ExponentialBuckets(0.001, 2, 16)
// covering 1ms–33s. This replaces the prior custom 100ms–60s range. p95 at
// the alert threshold (30s) is still detectable; p99 for calls >33s falls into
// +Inf. File a go-kit follow-up for WithHistogramBuckets if tighter tail
// precision is needed.
type llmObs struct {
	reg *kitmetrics.Registry
}

// newLLMObs constructs an llmObs bound to reg. In production, reg is created
// by kitmetrics.NewPrometheusRegistry("gocode") in main.
func newLLMObs(reg *kitmetrics.Registry) *llmObs {
	return &llmObs{reg: reg}
}

// middleware is a kitllm.Middleware that records call counts and latency.
//
// Outcomes:
//   - "ok"            — successful response
//   - "unavailable"   — ErrUnavailable (no API key configured)
//   - "circuit_open"  — ErrCircuitOpen (circuit breaker tripped; fast-fail)
//   - "auth"          — HTTP 401/403
//   - "rate_limit"    — HTTP 429
//   - "server"        — HTTP 5xx
//   - "client"        — other HTTP 4xx
//   - "error"         — anything else (transport, context cancel, etc.)
func (o *llmObs) middleware(ctx context.Context, req *kitllm.ChatRequest, next func(context.Context, *kitllm.ChatRequest) (*kitllm.ChatResponse, error)) (*kitllm.ChatResponse, error) {
	start := time.Now()
	resp, err := next(ctx, req)
	elapsed := time.Since(start)
	outcome := classifyOutcome(err)
	// Counter: gocode_llm_calls_total{outcome=...}
	o.reg.Incr(kitmetrics.Label("llm_calls_total", "outcome", outcome))
	// Histogram: gocode_llm_request_seconds{outcome=...}
	o.reg.ObserveSeconds(kitmetrics.Label("llm_request_seconds", "outcome", outcome), elapsed)
	return resp, err
}

// classifyOutcome maps an LLM error to a bounded Prometheus label.
// Sentinel checks run before errors.As so breaker-wrapped errors
// (which carry no APIError envelope) are caught first.
func classifyOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, kitllm.ErrUnavailable) {
		return "unavailable"
	}
	if errors.Is(err, kitllm.ErrCircuitOpen) {
		return "circuit_open"
	}
	var apiErr *kitllm.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
			return "auth"
		case apiErr.StatusCode == 429:
			return "rate_limit"
		case apiErr.StatusCode >= 500:
			return "server"
		case apiErr.StatusCode >= 400:
			return "client"
		}
	}
	return "error"
}
