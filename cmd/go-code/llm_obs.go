package main

import (
	"context"
	"errors"
	"time"

	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	kitllm "github.com/anatolykoptev/go-kit/llm"
	"github.com/prometheus/client_golang/prometheus"
)

// llmObs holds LLM call metrics registered against the application's registry.
// Constructed once in main via newLLMObs and threaded through registerTools.
//
// Counter: registered via the kitmetrics bridge so it gets the "gocode_" namespace
// prefix automatically → gocode_llm_calls_total{outcome=...} on /metrics.
//
// Histogram: registered directly against prometheus.DefaultRegisterer (same
// registry served by kitmetrics.MetricsHandler → promhttp.Handler). The bridge
// does not expose a public ObserveSeconds method, so for accurate per-outcome
// latency with custom buckets we follow the httpmw pattern and register directly.
// Name is "gocode_llm_request_seconds" to match the kitmetrics namespace convention.
type llmObs struct {
	reg  *kitmetrics.Registry
	hist *prometheus.HistogramVec
}

var (
	llmHistBuckets = []float64{0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 30, 60}
)

// newLLMObs constructs an llmObs bound to reg. In production, reg is created
// by kitmetrics.NewPrometheusRegistry("gocode") in main.
// The histogram is registered against prometheus.DefaultRegisterer, which is
// the same registry served by kitmetrics.MetricsHandler() → promhttp.Handler().
func newLLMObs(reg *kitmetrics.Registry) *llmObs {
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gocode_llm_request_seconds",
		Help:    "LLM Chat completion latency in seconds, labelled by outcome.",
		Buckets: llmHistBuckets,
	}, []string{"outcome"})
	// Best-effort registration: if already registered (e.g. test re-use),
	// retrieve the existing collector rather than panicking.
	if err := prometheus.DefaultRegisterer.Register(hist); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.HistogramVec); ok {
				hist = existing
			}
		}
	}
	return &llmObs{reg: reg, hist: hist}
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
	elapsed := time.Since(start).Seconds()
	outcome := classifyOutcome(err)
	// Counter via kitmetrics bridge: registers as gocode_llm_calls_total{outcome=...}
	o.reg.Incr(kitmetrics.Label("llm_calls_total", "outcome", outcome))
	// Histogram via direct prometheus: gocode_llm_request_seconds{outcome=...}
	o.hist.WithLabelValues(outcome).Observe(elapsed)
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
