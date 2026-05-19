package main

import (
	"context"
	"errors"
	"sort"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	kitllm "github.com/anatolykoptev/go-kit/llm"
)

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "ok"},
		{"unavailable", kitllm.ErrUnavailable, "unavailable"},
		{"circuit_open", kitllm.ErrCircuitOpen, "circuit_open"},
		{"401 auth", &kitllm.APIError{StatusCode: 401}, "auth"},
		{"403 auth", &kitllm.APIError{StatusCode: 403}, "auth"},
		{"429 rate_limit", &kitllm.APIError{StatusCode: 429}, "rate_limit"},
		{"500 server", &kitllm.APIError{StatusCode: 500}, "server"},
		{"502 server", &kitllm.APIError{StatusCode: 502}, "server"},
		{"400 client", &kitllm.APIError{StatusCode: 400}, "client"},
		{"404 client", &kitllm.APIError{StatusCode: 404}, "client"},
		{"generic error", errors.New("boom"), "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyOutcome(c.err); got != c.want {
				t.Errorf("classifyOutcome(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

func TestLLMObs_CallsNextAndPropagatesResult(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newLLMObs(reg)

	var nextCalled bool
	wantResp := &kitllm.ChatResponse{}
	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		nextCalled = true
		return wantResp, nil
	}

	resp, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("middleware did not call next")
	}
	if resp != wantResp {
		t.Fatal("middleware did not return next's response")
	}
}

func TestLLMObs_PropagatesError(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newLLMObs(reg)

	wantErr := errors.New("downstream failure")
	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		return nil, wantErr
	}

	_, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// TestLLMObs_CounterIncrements verifies that the counter is incremented
// in the registry on each call, using the labeled kitmetrics syntax.
func TestLLMObs_CounterIncrements(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newLLMObs(reg)

	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		return &kitllm.ChatResponse{}, nil
	}

	if _, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// kitmetrics stores labeled counters under the full labeled name key.
	got := reg.Value("llm_calls_total{outcome=ok}")
	if got != 2 {
		t.Errorf("llm_calls_total{outcome=ok} = %d, want 2", got)
	}
}

// TestLLMObs_ErrorOutcomeLabeled verifies the error outcome label is set correctly.
func TestLLMObs_ErrorOutcomeLabeled(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newLLMObs(reg)

	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		return nil, kitllm.ErrCircuitOpen
	}

	if _, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next); err == nil {
		t.Fatal("expected error, got nil")
	}

	got := reg.Value("llm_calls_total{outcome=circuit_open}")
	if got != 1 {
		t.Errorf("llm_calls_total{outcome=circuit_open} = %d, want 1", got)
	}
	// ok counter must remain at 0.
	if ok := reg.Value("llm_calls_total{outcome=ok}"); ok != 0 {
		t.Errorf("llm_calls_total{outcome=ok} = %d, want 0", ok)
	}
}

// TestLLMObs_HistogramVisibleInDefaultGatherer verifies that after a middleware
// call the latency histogram appears in prometheus.DefaultGatherer under the
// family name "<ns>_llm_request_seconds".  This pins the metric-name invariant
// that Prometheus alerts depend on (gocode_llm_request_seconds_bucket{...}).
//
// Uses a unique namespace ("gocodetest") so the registration does not collide
// with other tests that run in the same go test binary against DefaultRegisterer.
func TestLLMObs_HistogramVisibleInDefaultGatherer(t *testing.T) {
	const ns = "gocodetest"
	reg := kitmetrics.NewPrometheusRegistry(ns)
	obs := newLLMObs(reg)

	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		return &kitllm.ChatResponse{}, nil
	}
	if _, err := obs.middleware(context.Background(), &kitllm.ChatRequest{}, next); err != nil {
		t.Fatalf("middleware returned unexpected error: %v", err)
	}

	wantFamily := ns + "_llm_request_seconds"
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("DefaultGatherer.Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == wantFamily {
			// Found. Assert at least one sample was recorded.
			for _, m := range mf.GetMetric() {
				if h := m.GetHistogram(); h != nil && h.GetSampleCount() > 0 {
					return // test passes
				}
			}
			t.Fatalf("histogram family %q found but sample count is 0", wantFamily)
		}
	}
	t.Fatalf("metric family %q not found in DefaultGatherer; family names seen: %v",
		wantFamily, metricFamilyNames(mfs))
}

// metricFamilyNames returns a sorted slice of names for diagnostic output.
func metricFamilyNames(mfs []*dto.MetricFamily) []string {
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	sort.Strings(names)
	return names
}
