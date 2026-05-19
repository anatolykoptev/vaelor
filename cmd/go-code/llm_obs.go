package main

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	kitllm "github.com/anatolykoptev/go-kit/llm"
)

var (
	llmCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "go_code_llm_calls_total",
		Help: "LLM Chat completion calls, labelled by outcome.",
	}, []string{"outcome"})

	llmRequestSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "go_code_llm_request_seconds",
		Help:    "LLM Chat completion latency.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 100ms…~50s
	}, []string{"outcome"})
)

// llmMetricsMW is a kitllm.Middleware that records call counts and latency.
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
func llmMetricsMW(ctx context.Context, req *kitllm.ChatRequest, next func(context.Context, *kitllm.ChatRequest) (*kitllm.ChatResponse, error)) (*kitllm.ChatResponse, error) {
	start := time.Now()
	resp, err := next(ctx, req)
	elapsed := time.Since(start).Seconds()
	outcome := classifyOutcome(err)
	llmCallsTotal.WithLabelValues(outcome).Inc()
	llmRequestSeconds.WithLabelValues(outcome).Observe(elapsed)
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
