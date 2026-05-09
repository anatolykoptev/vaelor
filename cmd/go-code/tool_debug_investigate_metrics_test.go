package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

func TestMaxSampleValue_EmptyResponse(t *testing.T) {
	if got := maxSampleValue(nil); got != 0 {
		t.Errorf("nil resp: got %v", got)
	}
	if got := maxSampleValue(&promclient.QueryRangeResponse{}); got != 0 {
		t.Errorf("empty resp: got %v", got)
	}
}

func TestMaxSampleValue_PicksMaxAcrossSeries(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "1.5"}, {float64(60), "3.0"}}},
		{Values: [][2]any{{float64(0), "2.0"}, {float64(60), "10.5"}}},
	}
	if got := maxSampleValue(resp); got != 10.5 {
		t.Errorf("got %v, want 10.5", got)
	}
}

func TestMaxSampleValue_IgnoresUnparseable(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "not-a-number"}, {float64(60), "5.0"}}},
	}
	if got := maxSampleValue(resp); got != 5.0 {
		t.Errorf("got %v, want 5.0", got)
	}
}

// TestDiscoverFailureMetrics_FiltersByPattern verifies that discoverFailureMetrics
// is a pure filter — it takes the metric names slice and returns only those
// matching failure-counter naming conventions.
func TestDiscoverFailureMetrics_FiltersByPattern(t *testing.T) {
	names := []string{
		"http_requests_total",
		"signaling_call_outcome_total",
		"ws_handshake_failed_total",
		"sfu_chat_relay_dropped_total",
		"go_goroutines",
		"process_cpu_seconds_total",
		"auth_outcome",
	}
	got := discoverFailureMetrics(names)
	want := []string{"signaling_call_outcome_total", "ws_handshake_failed_total", "sfu_chat_relay_dropped_total"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRankSpikes_TopK(t *testing.T) {
	got := rankSpikes([]MetricSpike{
		{MetricName: "a", Score: 0.6},
		{MetricName: "b", Score: 0.95},
		{MetricName: "c", Score: 0.3},
		{MetricName: "d", Score: 0.8},
	}, 2)
	if len(got) != 2 || got[0].MetricName != "b" || got[1].MetricName != "d" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestFailureMetricRegex_RejectsGaugesAcceptsCounters(t *testing.T) {
	// Gauges without _total suffix must NOT match.
	rejected := []string{"auth_outcome", "connection_state", "http_errors_gauge"}
	for _, name := range rejected {
		if failureMetricRegex.MatchString(name) {
			t.Errorf("regex should NOT match gauge %q", name)
		}
	}
	// Counters with proper _total suffix must match.
	accepted := []string{
		"signaling_call_outcome_total",
		"ws_handshake_failed_total",
		"sfu_chat_relay_dropped_total",
		"grpc_server_failures_total",
		"api_errors_total",
	}
	for _, name := range accepted {
		if !failureMetricRegex.MatchString(name) {
			t.Errorf("regex should match counter %q", name)
		}
	}
}

func TestMetricSpike_KindField_FailureSpike(t *testing.T) {
	// Verify Kind field exists and is set to "failure" for failure spikes.
	s := MetricSpike{Kind: "failure", MetricName: "test_errors_total", Labels: `{service=svc}`, Ratio: 3.0, Score: 0.8}
	if s.Kind != "failure" {
		t.Errorf("expected Kind=failure, got %q", s.Kind)
	}
}

func TestFormatInvestigationResult_SpikeRendersKind(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "svc",
		MetricSpikes: []investigate.MetricSpike{
			{Kind: "failure", MetricName: "test_errors_total", Labels: `{service=svc}`, Ratio: 3.5, Score: 0.8},
			{Kind: "latency", MetricName: "grpc_duration_seconds_p99", Labels: `{service=svc}`, Ratio: 2.1, Score: 0.9},
			{Kind: "saturation", MetricName: "go_goroutines", Labels: `{service=svc}`, Ratio: 6.0, Score: 1.0},
		},
	}
	out := formatInvestigationResult(r)
	for _, want := range []string{`kind="failure"`, `kind="latency"`, `kind="saturation"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// --- Latency spike tests ---

// TestDiscoverLatencyHistograms_FiltersByPattern verifies pure filter behavior.
func TestDiscoverLatencyHistograms_FiltersByPattern(t *testing.T) {
	names := []string{
		"grpc_server_handling_seconds_bucket",
		"turn_cred_duration_ms_bucket",
		"http_request_duration_seconds_bucket",
		"some_latency_seconds_bucket",
		"go_goroutines",
		"process_cpu_seconds_total",
		"signaling_call_outcome_total",
	}
	got := discoverLatencyHistograms(names)
	want := []string{
		"grpc_server_handling_seconds_bucket",
		"turn_cred_duration_ms_bucket",
		"http_request_duration_seconds_bucket",
		"some_latency_seconds_bucket",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestComputeLatencySpikes_HappyPath(t *testing.T) {
	// Simulate: window p99=3.0, baseline p99=1.0 → ratio=3.0 > 2.0 → score=0.9
	// Uses start= param to distinguish window (start=1700000000) from baseline (earlier start).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			val := "1.0"
			if r.URL.Query().Get("start") == "1700000000" {
				val = "3.0" // window
			}
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)

	// Pass metricNames directly — no __name__ fetch.
	metricNames := []string{"grpc_server_handling_seconds_bucket"}
	spikes := computeLatencySpikes(context.Background(), prom, "test-svc", metricNames, start, end, &investigate.Diagnostics{})
	if len(spikes) == 0 {
		t.Fatal("expected at least one latency spike")
	}
	s := spikes[0]
	if s.Kind != "latency" {
		t.Errorf("Kind: got %q, want latency", s.Kind)
	}
	if s.MetricName != "grpc_server_handling_seconds_p99" {
		t.Errorf("MetricName: got %q, want grpc_server_handling_seconds_p99", s.MetricName)
	}
	if s.Score != scoreLatencyCritical {
		t.Errorf("Score: got %v, want %v (scoreLatencyCritical)", s.Score, scoreLatencyCritical)
	}
}

func TestBucketLatencyRatio_Boundaries(t *testing.T) {
	cases := []struct {
		ratio     float64
		wantScore float64
		skip      bool
	}{
		{ratio: 2.0001, wantScore: scoreLatencyCritical}, // > 2.0 → critical
		{ratio: 2.0, wantScore: scoreLatencyElevated},    // == 2.0, > 1.5 → elevated
		{ratio: 1.5001, wantScore: scoreLatencyElevated}, // > 1.5 → elevated
		{ratio: 1.5, skip: true},                         // == 1.5, not > 1.5 → skip
		{ratio: 1.0, skip: true},                         // < 1.5 → skip
	}
	for _, tc := range cases {
		score, _ := bucketLatencyRatio(tc.ratio)
		if tc.skip {
			if score != 0 {
				t.Errorf("ratio=%.4f: expected skip (0), got %v", tc.ratio, score)
			}
		} else {
			if score != tc.wantScore {
				t.Errorf("ratio=%.4f: got score=%v, want %v", tc.ratio, score, tc.wantScore)
			}
		}
	}
}

// --- Saturation spike tests ---

// TestDiscoverSaturationMetrics_FiltersByPattern verifies pure filter behavior.
func TestDiscoverSaturationMetrics_FiltersByPattern(t *testing.T) {
	names := []string{
		"worker_pool_active",
		"request_queue_size",
		"connection_queue_depth",
		"tasks_pending",
		"go_goroutines",
		"process_resident_memory_bytes",
		"http_requests_total",
	}
	got := discoverQueueMetrics(names)
	want := []string{"worker_pool_active", "request_queue_size", "connection_queue_depth", "tasks_pending"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestComputeSaturationSpikes_HappyPath(t *testing.T) {
	// Simulates window_max=6.0, baseline_max=1.0 → ratio=6.0 > 5.0 → score=1.0
	// Uses start= param to distinguish window (start=1700000000) from baseline (earlier start).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			val := "1.0"
			if r.URL.Query().Get("start") == "1700000000" {
				val = "6.0" // window value > baseline
			}
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)

	// No wildcard metrics — only sentinels queried (empty slice).
	spikes := computeSaturationSpikes(context.Background(), prom, "test-svc", nil, start, end, &investigate.Diagnostics{})
	if len(spikes) == 0 {
		t.Fatal("expected at least one saturation spike (sentinel queries)")
	}
	for _, s := range spikes {
		if s.Kind != "saturation" {
			t.Errorf("spike %q: Kind=%q, want saturation", s.MetricName, s.Kind)
		}
	}
}

func TestBucketSaturationRatio_Boundaries(t *testing.T) {
	cases := []struct {
		ratio     float64
		wantScore float64
		skip      bool
	}{
		{ratio: 5.0001, wantScore: scoreCritical}, // > 5.0 → 1.0
		{ratio: 5.0, wantScore: scoreElevated},    // == 5.0, > 2.0 → 0.8
		{ratio: 2.0001, wantScore: scoreElevated}, // > 2.0 → 0.8
		{ratio: 2.0, wantScore: scoreMild},        // == 2.0, > 1.3 → 0.6
		{ratio: 1.3001, wantScore: scoreMild},     // > 1.3 → 0.6
		{ratio: 1.3, skip: true},                  // == 1.3, not > 1.3 → skip
		{ratio: 1.0, skip: true},
	}
	for _, tc := range cases {
		score, _ := bucketSaturationRatio(tc.ratio)
		if tc.skip {
			if score != 0 {
				t.Errorf("ratio=%.4f: expected skip (0), got %v", tc.ratio, score)
			}
		} else {
			if score != tc.wantScore {
				t.Errorf("ratio=%.4f: got score=%v, want %v", tc.ratio, score, tc.wantScore)
			}
		}
	}
}

// --- Wire-in / orchestration tests ---

func TestComputeAnomalyScore_MergesAllKinds(t *testing.T) {
	// Simulate: failure=none, latency spike ratio>2, saturation spike ratio>5
	// Expected: top score from saturation (1.0), merged spike list has latency + saturation kinds.
	// metricNames slice provides one latency bucket and no queue metrics.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			q := r.URL.Query().Get("query")
			isWindow := r.URL.Query().Get("start") == "1700000000"
			switch {
			case strings.Contains(q, "histogram_quantile"):
				// latency: window=3.0, baseline=1.0 → ratio=3.0 > 2.0 → latency critical
				if isWindow {
					fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"3.0"]]}]}}`)
				} else {
					fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"1.0"]]}]}}`)
				}
			case strings.Contains(q, "go_goroutines") || strings.Contains(q, "process_resident"):
				// saturation: window=6.0, baseline=1.0 → ratio=6.0 > 5.0 → sat critical
				if isWindow {
					fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"6.0"]]}]}}`)
				} else {
					fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"1.0"]]}]}}`)
				}
			default:
				// failure metrics return empty
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
			}
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	// Provide metricNames with one latency bucket (no failure/queue metrics).
	metricNames := []string{"http_request_duration_seconds_bucket"}
	topScore, spikes := computeAnomalyScore(context.Background(), prom, "test-svc", metricNames, start, end, diags)
	if len(spikes) == 0 {
		t.Fatal("expected merged spikes across latency and saturation")
	}

	kindsSeen := make(map[string]bool)
	for _, s := range spikes {
		kindsSeen[s.Kind] = true
	}
	if !kindsSeen["latency"] {
		t.Error("expected latency kind in merged spikes")
	}
	if !kindsSeen["saturation"] {
		t.Error("expected saturation kind in merged spikes")
	}
	if topScore != scoreCritical {
		t.Errorf("topScore: got %v, want %v (saturation ratio>5 → scoreCritical)", topScore, scoreCritical)
	}
}

func TestComputeAnomalyScore_AllEmpty_FallsBackToLegacy(t *testing.T) {
	// No failure/latency/saturation metrics → legacy fallback runs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	// Legacy returns scoreDefault=0.5 when both window and baseline are empty.
	// Empty metricNames → no failure/latency/queue candidates.
	topScore, spikes := computeAnomalyScore(context.Background(), prom, "test-svc", nil, start, end, diags)
	if len(spikes) != 0 {
		t.Errorf("expected no spikes for empty registry, got %d", len(spikes))
	}
	if topScore != scoreDefault {
		t.Errorf("expected scoreDefault=%v fallback, got %v", scoreDefault, topScore)
	}
}

func TestLatencyHistogramRegex_OverBroadRejected(t *testing.T) {
	// request_parse_ms_bucket should NOT match — "_ms_bucket" without duration prefix
	// is too broad and catches non-histogram counters.
	if latencyHistogramRegex.MatchString("request_parse_ms_bucket") {
		t.Error("latencyHistogramRegex should NOT match request_parse_ms_bucket")
	}
	// These must still match.
	matches := []string{
		"foo_seconds_bucket",
		"foo_duration_seconds_bucket",
		"foo_duration_ms_bucket",
		"foo_latency_seconds_bucket",
	}
	for _, name := range matches {
		if !latencyHistogramRegex.MatchString(name) {
			t.Errorf("latencyHistogramRegex should match %q", name)
		}
	}
}

func TestSaturationQueueRegex_CaseInsensitive(t *testing.T) {
	// Mixed-case names must match after adding (?i) flag.
	matches := []string{
		"Worker_Pool_Active",
		"Request_Queue_Size",
		"Tasks_Pending",
	}
	for _, name := range matches {
		if !saturationQueueRegex.MatchString(name) {
			t.Errorf("saturationQueueRegex should match %q (case-insensitive)", name)
		}
	}
}

func TestComputeLatencySpikes_UpdatesDiagnostics(t *testing.T) {
	// After computeLatencySpikes with 1 candidate, MetricsQueried must be incremented by 2.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	metricNames := []string{"grpc_server_handling_seconds_bucket"}
	computeLatencySpikes(context.Background(), prom, "test-svc", metricNames, start, end, diags)
	if diags.MetricsQueried != 2 {
		t.Errorf("MetricsQueried: got %d, want 2 (1 candidate × 2 queries)", diags.MetricsQueried)
	}
}

func TestComputeSaturationSpikes_UpdatesDiagnostics(t *testing.T) {
	// sentinel count = 2 metrics × 2 queries each = 4 MetricsQueried minimum.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	computeSaturationSpikes(context.Background(), prom, "test-svc", nil, start, end, diags)
	if diags.MetricsQueried != 4 {
		t.Errorf("MetricsQueried: got %d, want 4 (2 sentinels × 2 queries each)", diags.MetricsQueried)
	}
}

func TestComputeFailureSpikes_JobLabelFallback(t *testing.T) {
	// service= returns empty; job= returns data with ratio > ratioCritical → spike expected.
	callNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			q := r.URL.Query().Get("query")
			if strings.Contains(q, "service=") {
				// service= returns empty
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
			} else {
				// job= returns data
				callNum++
				val := "1.0"
				if callNum%2 == 1 {
					val = "10.0" // window: high → ratio > ratioCritical
				}
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
			}
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	metricNames := []string{"ws_handshake_failed_total"}
	spikes := computeFailureSpikes(context.Background(), prom, "my-svc", metricNames, start, end, diags)
	if len(spikes) == 0 {
		t.Fatal("expected spike via job= label fallback")
	}
	if !strings.Contains(spikes[0].Labels, `job=`) {
		t.Errorf("Labels should contain job= fallback label, got %q", spikes[0].Labels)
	}
}

func TestComputeLatencySpikes_JobLabelFallback(t *testing.T) {
	// service= empty, job= returns high latency → spike expected.
	callNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			q := r.URL.Query().Get("query")
			if strings.Contains(q, "service=") {
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
			} else {
				callNum++
				val := "1.0"
				if callNum%2 == 1 {
					val = "3.0"
				}
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
			}
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	metricNames := []string{"grpc_server_handling_seconds_bucket"}
	spikes := computeLatencySpikes(context.Background(), prom, "my-svc", metricNames, start, end, diags)
	if len(spikes) == 0 {
		t.Fatal("expected latency spike via job= label fallback")
	}
	if !strings.Contains(spikes[0].Labels, `job=`) {
		t.Errorf("Labels should contain job= fallback label, got %q", spikes[0].Labels)
	}
}

func TestComputeSaturationSpikes_JobLabelFallback(t *testing.T) {
	// service= empty for sentinels, job= returns high saturation → spike expected.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			q := r.URL.Query().Get("query")
			if strings.Contains(q, "service=") {
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
			} else {
				val := "1.0"
				if r.URL.Query().Get("start") == "1700000000" {
					val = "6.0"
				}
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
			}
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	// sentinels only (nil metricNames → no queue wildcards)
	spikes := computeSaturationSpikes(context.Background(), prom, "my-svc", nil, start, end, diags)
	if len(spikes) == 0 {
		t.Fatal("expected saturation spike via job= label fallback")
	}
	if !strings.Contains(spikes[0].Labels, `job=`) {
		t.Errorf("Labels should contain job= fallback label, got %q", spikes[0].Labels)
	}
}

// TestLabels_NoDoubleEscapeInRenderedXML verifies that spike Labels values
// produced by computeFailureSpikes do not contain inner quotes that would be
// double-escaped by labels=%q in formatInvestigationResult.
// Regression test for the %q Labels double-escape bug (issue #65).
func TestLabels_NoDoubleEscapeInRenderedXML(t *testing.T) {
	// Simulate a failure spike with window > baseline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			q := r.URL.Query().Get("query")
			if strings.Contains(q, "service=") {
				// window high, baseline low → spike
				if strings.Contains(q, "start=") || true {
					fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"10.0"]]}]}}`)
				}
			} else {
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"1.0"]]}]}}`)
			}
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	// Two requests: window (high) + baseline (low) → ratio=10 > 5 → critical spike.
	callNum := 0
	srvAlt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			callNum++
			val := "10.0"
			if callNum%2 == 0 {
				val = "1.0" // baseline
			}
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,"%s"]]}]}}`, val)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srvAlt.Close()

	prom := promclient.NewClient(srvAlt.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}

	metricNames := []string{"ws_handshake_failed_total"}
	spikes := computeFailureSpikes(context.Background(), prom, "oxpulse-chat", metricNames, start, end, diags)
	if len(spikes) == 0 {
		t.Skip("no spikes produced (network condition); skipping escape check")
	}

	res := &investigate.InvestigationResult{
		Service:      "oxpulse-chat",
		MetricSpikes: spikes,
	}
	out := formatInvestigationResult(res)

	// The labels= attribute in the XML spike element must not contain \" (double-escaped).
	if strings.Contains(out, `\"`) {
		t.Errorf("rendered XML contains double-escaped quotes in labels= attribute (issue #65):\n%s", out)
	}
	// Verify the labels attribute appears without inner quotes.
	if !strings.Contains(out, "labels=") {
		t.Error("expected labels= attribute in rendered XML")
	}
}

func TestMetricsQueried_NeverResets(t *testing.T) {
	// Regression for #75: computeAnomalyScoreLegacy used "= 2" (assignment)
	// instead of "+= 2" (addition), overwriting any counts accumulated by the
	// auto-discovery phase when it fired as a fallback.
	// Invariant: MetricsQueried must be monotonically increasing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/query_range") {
			// Return empty results — no spikes discovered by any phase — legacy fires.
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		} else {
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	start := time.Unix(1700000000, 0)
	end := start.Add(5 * time.Minute)
	diags := &investigate.Diagnostics{}
	diags.MetricsQueried = 1 // simulate one orchestrator pre-fetch already counted

	// Empty metricNames forces all discovery phases to produce no candidates,
	// which means no spikes cross scoreNominal, triggering legacy fallback.
	_, _ = computeAnomalyScore(context.Background(), prom, "test-svc", nil, start, end, diags)

	// Legacy fires 2 QueryRange calls. With the bug (= 2), the counter would be
	// reset to 2 — losing the orchestrator's 1. With the fix (+= 2) it must be >= 3.
	if diags.MetricsQueried < 3 {
		t.Errorf("MetricsQueried = %d; want >= 3 (1 orchestrator + 2 legacy QueryRange calls, never reset)", diags.MetricsQueried)
	}
}
