// cmd/go-code/tool_debug_investigate_sprint_a_test.go
// Tests for Sprint A: parallel Prom queries + skip LLM when no signal.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/llm"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// ---- Sprint A.1: parallel Prom queries ----

// TestComputeFailureSpikes_Parallel_Race runs 20 candidates concurrently and
// verifies the race detector finds no data races. Mixed results: half anomalous
// (ratio > ratioCritical), half nominal. Validates all expected spikes are
// present and MetricsQueried is correct.
func TestComputeFailureSpikes_Parallel_Race(t *testing.T) {
	// 20 distinct failure metric names.
	const numCandidates = 20

	// All metric names must match failureMetricRegex to be picked up.
	names := make([]string, numCandidates)
	for i := range names {
		names[i] = fmt.Sprintf("svc_err%02d_errors_total", i)
	}

	// For even-indexed metrics (0,2,...18): window=10, baseline=1 → ratio=10 > 5 → critical.
	// For odd-indexed  metrics (1,3,...19): window=0,  baseline=0 → nominal, no spike.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			http.Error(w, "not found", 404)
			return
		}
		q := r.URL.Query().Get("query")
		// Determine which metric this query targets.
		val := "0.0" // default: no data (odd-indexed)
		for i := 0; i < numCandidates; i += 2 {
			metricName := fmt.Sprintf("svc_err%02d_errors_total", i)
			if strings.Contains(q, metricName) {
				// window (start present) gets 10, baseline gets 1.
				// Both window and baseline calls share the same handler.
				// We need to distinguish window vs baseline. Use start param:
				// window start = 1700000000, baseline start is earlier.
				startStr := r.URL.Query().Get("start")
				if startStr == "1700000000" {
					val = "10.0"
				} else {
					val = "1.0"
				}
				break
			}
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	spikes := computeFailureSpikes(context.Background(), prom, "test-svc", names, start, end, diags)

	// 10 anomalous metrics expected (even-indexed: 0,2,...18).
	if len(spikes) != 10 {
		t.Errorf("expected 10 spikes, got %d", len(spikes))
	}
	// MetricsQueried: even candidates return non-zero (no fallback) = 2 RPCs each;
	// odd candidates return 0.0 (isEmpty=true → job= fallback fires) = 4 RPCs each.
	// 10 even × 2 + 10 odd × 4 = 60.
	if diags.MetricsQueried != 60 {
		t.Errorf("MetricsQueried = %d, want 60 (10 even × 2 RPCs + 10 odd × 4 RPCs with job= fallback)", diags.MetricsQueried)
	}
	// All spikes must have Kind="failure".
	for _, s := range spikes {
		if s.Kind != "failure" {
			t.Errorf("spike %q: Kind=%q, want failure", s.MetricName, s.Kind)
		}
	}
}

// TestComputeFailureSpikes_Parallel_OrderingDeterministic verifies that same
// input produces the same set of spikes across runs (order-independent comparison
// via sort by metric name).
func TestComputeFailureSpikes_Parallel_OrderingDeterministic(t *testing.T) {
	names := []string{
		"svc_errors_total",
		"svc_failures_total",
		"svc_dropped_total",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			http.Error(w, "not found", 404)
			return
		}
		// All three spike: window=10, baseline=1.
		q := r.URL.Query().Get("query")
		startStr := r.URL.Query().Get("start")
		_ = q
		val := "1.0"
		if startStr == "1700000000" {
			val = "10.0"
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)

	run := func() []string {
		spikes := computeFailureSpikes(context.Background(), prom, "test-svc", names, start, end, &investigate.Diagnostics{})
		nameList := make([]string, len(spikes))
		for i, s := range spikes {
			nameList[i] = s.MetricName
		}
		sort.Strings(nameList)
		return nameList
	}

	first := run()
	for i := 0; i < 5; i++ {
		got := run()
		if len(got) != len(first) {
			t.Fatalf("run %d: len=%d, want %d", i+1, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Errorf("run %d: spike[%d]=%q, want %q", i+1, j, got[j], first[j])
			}
		}
	}
}

// TestComputeLatencySpikes_Parallel runs 15 latency candidates in parallel and
// verifies all anomalous ones are returned with correct diagnostics count.
func TestComputeLatencySpikes_Parallel(t *testing.T) {
	const numCandidates = 15

	names := make([]string, numCandidates)
	for i := range names {
		names[i] = fmt.Sprintf("svc_op%02d_duration_seconds_bucket", i)
	}

	// Even-indexed: ratio=3.0 (window=3, baseline=1) → latency critical.
	// Odd-indexed:  no baseline → skipped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			http.Error(w, "not found", 404)
			return
		}
		q := r.URL.Query().Get("query")
		startStr := r.URL.Query().Get("start")
		for i := 0; i < numCandidates; i += 2 {
			metricName := fmt.Sprintf("svc_op%02d_duration_seconds_bucket", i)
			if strings.Contains(q, metricName) {
				val := "1.0"
				if startStr == "1700000000" {
					val = "3.0"
				}
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
				return
			}
		}
		// Odd-indexed: return empty (no baseline → skipped by computeLatencySpikes).
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	spikes := computeLatencySpikes(context.Background(), prom, "test-svc", names, start, end, diags)

	// Even-indexed: 0,2,4,6,8,10,12,14 = 8 spikes.
	if len(spikes) != 8 {
		t.Errorf("expected 8 latency spikes, got %d", len(spikes))
	}
	// MetricsQueried: even candidates return data (no fallback) = 2 RPCs each;
	// odd candidates return empty result (isEmpty=true → job= fallback fires) = 4 RPCs each.
	// 8 even × 2 + 7 odd × 4 = 44.
	if diags.MetricsQueried != 44 {
		t.Errorf("MetricsQueried = %d, want 44 (8 even × 2 RPCs + 7 odd × 4 RPCs with job= fallback)", diags.MetricsQueried)
	}
	for _, s := range spikes {
		if s.Kind != "latency" {
			t.Errorf("spike %q: Kind=%q, want latency", s.MetricName, s.Kind)
		}
	}
}

// TestComputeSaturationSpikes_Parallel verifies that both the sentinel loop
// and the wildcard-queue loop run in parallel (10 queue wildcards).
func TestComputeSaturationSpikes_Parallel(t *testing.T) {
	const numQueueMetrics = 10

	names := make([]string, numQueueMetrics)
	for i := range names {
		names[i] = fmt.Sprintf("worker%02d_queue_size", i)
	}

	// All queue metrics: window=6, baseline=1 → ratio=6 > 5 → critical.
	// Sentinels: window=6, baseline=1 → ratio=6 > 5 → critical.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			http.Error(w, "not found", 404)
			return
		}
		startStr := r.URL.Query().Get("start")
		val := "1.0"
		if startStr == "1700000000" {
			val = "6.0"
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	spikes := computeSaturationSpikes(context.Background(), prom, "test-svc", names, start, end, diags)

	// 2 sentinels + 10 queue = 12 spikes.
	if len(spikes) != 12 {
		t.Errorf("expected 12 saturation spikes (2 sentinels + 10 queue), got %d", len(spikes))
	}
	// MetricsQueried = 2*(2 sentinels) + 2*(10 queue) = 24.
	if diags.MetricsQueried != 24 {
		t.Errorf("MetricsQueried = %d, want 24 (2 sentinels + 10 queue × 2 each)", diags.MetricsQueried)
	}
}

// TestComputeFailureSpikes_Parallel_ActualConcurrency verifies that queries
// actually execute concurrently, not serially. Tracks peak concurrent in-flight
// requests: with promQueryConcurrency=10 workers and numCandidates=10, all 10
// window queries should be in-flight at the same time. A serial implementation
// would have peak=1.
func TestComputeFailureSpikes_Parallel_ActualConcurrency(t *testing.T) {
	const numCandidates = promQueryConcurrency // exactly fill the worker pool

	// Names to pass directly as failure metrics.
	names := make([]string, numCandidates)
	for i := range names {
		names[i] = fmt.Sprintf("barrier_err%02d_errors_total", i)
	}

	var (
		inFlight   int32
		peakFlight int32
	)

	// slowRespond adds a small artificial delay to let all goroutines start
	// before any returns. Without it, fast machines might serialize due to
	// scheduling. 5ms is sufficient; shorter than 20ms reduces total test time.
	const perReqDelay = 5 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			http.Error(w, "not found", 404)
			return
		}
		cur := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		// Update peak.
		for {
			old := atomic.LoadInt32(&peakFlight)
			if cur <= old || atomic.CompareAndSwapInt32(&peakFlight, old, cur) {
				break
			}
		}
		time.Sleep(perReqDelay)
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"0.0"]]}]}}`)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)

	computeFailureSpikes(context.Background(), prom, "test-svc", names, start, end, &investigate.Diagnostics{})

	peak := atomic.LoadInt32(&peakFlight)
	// With 10 candidates (= promQueryConcurrency) all starting simultaneously,
	// peak concurrent in-flight should be well above 1. Floor of 5 distinguishes
	// genuine parallelism from lucky scheduling on a serial implementation.
	if peak < 5 {
		t.Errorf("peak concurrent in-flight requests = %d; want >= 5 (serial execution suspected)", peak)
	}
}

// ---- Sprint A.2: skip LLM when no signal ----

// fakePanicLLM2 panics if Complete is called — ensures LLM is NOT called.
type fakePanicLLM2 struct{}

func (f *fakePanicLLM2) Complete(_ context.Context, _, _ string, _ ...llm.ChatOption) (string, error) {
	panic("LLM.Complete must not be called when there is no signal")
}

// fakeCaptureLLM2 records whether Complete was called and returns a preset summary.
type fakeCaptureLLM2 struct {
	mu      sync.Mutex
	called  bool
	summary string
}

func (f *fakeCaptureLLM2) Complete(_ context.Context, _, _ string, _ ...llm.ChatOption) (string, error) {
	f.mu.Lock()
	f.called = true
	f.mu.Unlock()
	return f.summary, nil
}

// TestRunLLMPhase_NoSignal_SkipsCall verifies that when hypotheses, spikes,
// and alerts are all empty, the LLM is NOT called and LLMSkippedReason is set.
func TestRunLLMPhase_NoSignal_SkipsCall(t *testing.T) {
	res := &investigate.InvestigationResult{
		Service:         "quiet-svc",
		Hypotheses:      nil,
		MetricSpikes:    nil,
		AlertViolations: nil,
	}
	input := DebugInvestigateInput{Service: "quiet-svc", StartUnix: 100, EndUnix: 200}

	// fakePanicLLM2 panics if called — test would panic if LLM is invoked.
	runLLMPhaseInner(context.Background(), &fakePanicLLM2{}, nil, nil, input, nil, nil,
		time.Unix(100, 0), time.Unix(200, 0), res)

	if res.Diagnostics.LLMSkippedReason != "no_signal" {
		t.Errorf("LLMSkippedReason = %q, want %q", res.Diagnostics.LLMSkippedReason, "no_signal")
	}
	if res.LLMSummary != "" {
		t.Errorf("LLMSummary should be empty on no-signal skip, got %q", res.LLMSummary)
	}
}

// TestRunLLMPhase_HasSpike_RunsCall verifies that when MetricSpikes are present
// (but Hypotheses is empty), the LLM IS called (because there is a signal).
func TestRunLLMPhase_HasSpike_RunsCall(t *testing.T) {
	fakeLLM := &fakeCaptureLLM2{summary: "spike summary"}
	res := &investigate.InvestigationResult{
		Service:    "noisy-svc",
		Hypotheses: nil, // no hypotheses
		MetricSpikes: []investigate.MetricSpike{
			{Kind: "failure", MetricName: "svc_errors_total", Labels: `{service=noisy-svc}`, Ratio: 5.0, Score: 1.0},
		},
		AlertViolations: nil,
	}
	input := DebugInvestigateInput{Service: "noisy-svc", StartUnix: 100, EndUnix: 200}

	runLLMPhaseInner(context.Background(), fakeLLM, nil, nil, input, nil, nil,
		time.Unix(100, 0), time.Unix(200, 0), res)

	if !fakeLLM.called {
		t.Error("LLM.Complete should have been called when MetricSpikes are present")
	}
	if res.Diagnostics.LLMSkippedReason != "" {
		t.Errorf("LLMSkippedReason should be empty when LLM runs, got %q", res.Diagnostics.LLMSkippedReason)
	}
}

// TestRunLLMPhase_HasHypothesis_RunsCall verifies that when Hypotheses are present,
// LLM is called as before.
func TestRunLLMPhase_HasHypothesis_RunsCall(t *testing.T) {
	fakeLLM := &fakeCaptureLLM2{summary: "hypothesis summary"}
	res := &investigate.InvestigationResult{
		Service: "hyp-svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "HandleReq", AnomalyScore: 0.9},
		},
		MetricSpikes:    nil,
		AlertViolations: nil,
	}
	input := DebugInvestigateInput{Service: "hyp-svc", StartUnix: 100, EndUnix: 200}

	runLLMPhaseInner(context.Background(), fakeLLM, nil, nil, input, nil, nil,
		time.Unix(100, 0), time.Unix(200, 0), res)

	if !fakeLLM.called {
		t.Error("LLM.Complete should have been called when Hypotheses are present")
	}
}

// TestRunLLMPhase_HasAlert_RunsCall verifies that when AlertViolations are present,
// LLM is called (alert = signal).
func TestRunLLMPhase_HasAlert_RunsCall(t *testing.T) {
	fakeLLM := &fakeCaptureLLM2{summary: "alert summary"}
	res := &investigate.InvestigationResult{
		Service:      "alert-svc",
		Hypotheses:   nil,
		MetricSpikes: nil,
		AlertViolations: []investigate.AlertViolation{
			{AlertName: "HighErrorRate", Severity: "critical", Service: "alert-svc"},
		},
	}
	input := DebugInvestigateInput{Service: "alert-svc", StartUnix: 100, EndUnix: 200}

	runLLMPhaseInner(context.Background(), fakeLLM, nil, nil, input, nil, nil,
		time.Unix(100, 0), time.Unix(200, 0), res)

	if !fakeLLM.called {
		t.Error("LLM.Complete should have been called when AlertViolations are present")
	}
}

// TestRunLLMPhase_NilClient_SetsReason verifies that LLMSkippedReason is set
// when LLMHasKey=false (deps.LLM is nil → LLMHasKey defaults false).
// The outer runLLMPhase gate fires; runLLMPhaseInner is never called.
func TestRunLLMPhase_NilClient_SetsReason(t *testing.T) {
	res := &investigate.InvestigationResult{
		Service: "no-client-svc",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "HandleReq", AnomalyScore: 0.9},
		},
	}
	input := DebugInvestigateInput{Service: "no-client-svc", StartUnix: 100, EndUnix: 200}

	// Call runLLMPhase (not Inner) with nil deps.LLM — exercises the nil-client guard.
	runLLMPhase(context.Background(), analyze.Deps{LLM: nil}, nil, input, nil, nil,
		time.Unix(100, 0), time.Unix(200, 0), res)

	if res.Diagnostics.LLMSkippedReason != llmSkipReasonNoKey {
		t.Errorf("LLMSkippedReason = %q, want %q", res.Diagnostics.LLMSkippedReason, llmSkipReasonNoKey)
	}
}
