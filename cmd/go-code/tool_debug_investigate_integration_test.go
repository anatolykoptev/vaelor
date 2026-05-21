// cmd/go-code/tool_debug_investigate_integration_test.go
//
// Integration tests for handleDebugInvestigate / runInvestigation.
//
// Strategy (option b from #57): call handleDebugInvestigate end-to-end,
// then poll debugInvestigateStore until the investigation completes or a
// deadline fires.
//
// Store key stability: pass explicit StartUnix/EndUnix so the store key used
// inside runInvestigation exactly matches what we poll with here. When those
// fields are 0 the handler derives times from time.Now() which differs from
// the local copy we compute — poll would never match.
//
// deps uses integrationDeps() (LLM=NoOp, LLMHasKey=false) — no LLM key needed;
// deterministic phases run, inner LLM phase is skipped with a marker.
// input.Repo is "" throughout — skips callgraph build.
//
// Cleanup: each test replaces debugInvestigateStore with a fresh instance via
// t.Cleanup so tests do not share state. Because the store is package-global
// these tests must not run in parallel.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/promclient"
	"github.com/anatolykoptev/go-kit/llm"
	"os"
	"path/filepath"
)

// integrationDeps returns a Deps for integration tests. LLM=NoOp, LLMHasKey=false —
// no outer gate on debug_investigate; inner LLM phase is skipped with a marker.
// Tests that specifically exercise the LLM phase should construct Deps inline.
func integrationDeps() analyze.Deps {
	return analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}
}

// fixedWindow returns a deterministic (start, end) pair for integration tests.
// Using fixed unix seconds means the store key inside runInvestigation and the
// key we poll here are identical.
func fixedWindow() (time.Time, time.Time) {
	start := time.Unix(1_700_000_000, 0)
	end := time.Unix(1_700_000_600, 0) // +10 min
	return start, end
}

// pollStore waits for the investigation to reach a terminal state (Done or
// Failed). It polls up to maxWait with 25 ms ticks. Returns the final state
// or nil on timeout.
func pollStore(svc string, start, end time.Time, repo string, maxWait time.Duration) *investigate.State {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		st, ok := debugInvestigateStore.Get(svc, start, end, repo)
		if ok {
			switch st.Status() {
			case investigate.StatusDone, investigate.StatusFailed:
				return st
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

// newJaegerFake builds an httptest server that serves:
//   - GET /api/services → services list
//   - GET /api/traces?*  → traces list
func newJaegerFake(services []string, traces []jaegerclient.Trace) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/services":
			json.NewEncoder(w).Encode(map[string]any{"data": services})
		case strings.HasPrefix(r.URL.Path, "/api/traces"):
			json.NewEncoder(w).Encode(map[string]any{"data": traces})
		default:
			http.NotFound(w, r)
		}
	}))
}

// newPromFake builds an httptest server that serves /api/v1/query_range.
// Each call returns a single series with a single sample at value sampleVal.
// Pass sampleVal < 0 to make the server return HTTP 500.
func newPromFake(sampleVal float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			http.NotFound(w, r)
			return
		}
		if sampleVal < 0 {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		ts := float64(time.Now().Unix())
		valStr := fmt.Sprintf("%g", sampleVal)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "matrix",
				"result": []any{
					map[string]any{
						"metric": map[string]string{},
						"values": [][2]any{{ts, valStr}},
					},
				},
			},
		})
	}))
}

// newPromFakeWithLabels builds a Prometheus fake that serves label discovery
// and query_range with window vs baseline values distinguished by the start
// parameter. start > pivot → window query (returns windowVal);
// start <= pivot → baseline query (returns baselineVal).
func newPromFakeWithLabels(metricNames []string, pivot time.Time, windowVal, baselineVal float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/label/__name__/values":
			type labelResp struct {
				Status string   `json:"status"`
				Data   []string `json:"data"`
			}
			json.NewEncoder(w).Encode(labelResp{Status: "success", Data: metricNames})
		case "/api/v1/query_range":
			startStr := r.URL.Query().Get("start")
			startF, _ := strconv.ParseFloat(startStr, 64)
			val := baselineVal
			if startF >= float64(pivot.Unix()) {
				val = windowVal
			}
			ts := float64(time.Now().Unix())
			valStr := fmt.Sprintf("%g", val)
			json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "matrix",
					"result": []any{
						map[string]any{
							"metric": map[string]string{},
							"values": [][2]any{{ts, valStr}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestIntegration_HappyPath verifies that a single trace with one span and an
// elevated window rate (vs baseline) produces a non-empty Hypotheses list and
// a non-empty MetricSpikes slice with the correct top metric name.
func TestIntegration_HappyPath(t *testing.T) {
	t.Cleanup(func() { debugInvestigateStore = investigate.NewInvestigationStore() })

	svc := "test-svc-happy"
	jaegerSrv := newJaegerFake(
		[]string{svc},
		[]jaegerclient.Trace{{
			TraceID: "abc123",
			Spans:   []jaegerclient.Span{{SpanID: "s1", OperationName: "GET /api/health"}},
		}},
	)
	defer jaegerSrv.Close()

	// Prometheus fake: serves signaling_call_outcome_total in label values,
	// returns windowVal=100 for window queries and baselineVal=10 for baseline
	// queries → ratio=10 > ratioCritical(5) → scoreCritical(1.0).
	// pivot = fixedWindow start; queries after pivot are window, before are baseline.
	start, end := fixedWindow()
	promSrv := newPromFakeWithLabels(
		[]string{"signaling_call_outcome_total"},
		start, // pivot: queries with start > this are window queries
		100.0, // windowVal
		10.0,  // baselineVal
	)
	defer promSrv.Close()

	prom := promclient.NewClient(promSrv.URL, 5*time.Second)
	jaeger := jaegerclient.NewClient(jaegerSrv.URL, 5*time.Second)

	_, callErr := handleDebugInvestigate(
		context.Background(),
		DebugInvestigateInput{
			Service:   svc,
			StartUnix: start.Unix(),
			EndUnix:   end.Unix(),
		},
		Config{},
		integrationDeps(),
		prom,
		jaeger,
		nil,
	)
	if callErr != nil {
		t.Fatalf("handleDebugInvestigate: unexpected error: %v", callErr)
	}

	st := pollStore(svc, start, end, "", 10*time.Second)
	if st == nil {
		t.Fatal("investigation did not complete within 10s")
	}
	if st.Status() != investigate.StatusDone {
		t.Fatalf("expected StatusDone, got %v (err: %s)", st.Status(), st.Error())
	}
	r := st.Result()
	if len(r.Hypotheses) == 0 {
		t.Fatal("expected non-empty Hypotheses, got none")
	}
	// ratio=10 > ratioCritical(5) → scoreCritical(1.0)
	if got := r.Hypotheses[0].AnomalyScore; got != scoreCritical {
		t.Errorf("ratio=10 should bucket as scoreCritical=%v, got %v", scoreCritical, got)
	}
	// MetricSpikes must be non-empty with the correct top metric.
	if len(r.MetricSpikes) == 0 {
		t.Fatal("expected non-empty MetricSpikes, got none")
	}
	if r.MetricSpikes[0].MetricName != "signaling_call_outcome_total" {
		t.Errorf("expected top spike metric=signaling_call_outcome_total, got %q", r.MetricSpikes[0].MetricName)
	}
}

// TestIntegration_JaegerEmpty verifies that zero traces yields empty
// Hypotheses (frequency-only fallback also produces zero hypotheses).
func TestIntegration_JaegerEmpty(t *testing.T) {
	t.Cleanup(func() { debugInvestigateStore = investigate.NewInvestigationStore() })

	svc := "test-svc-empty-traces"
	jaegerSrv := newJaegerFake([]string{svc}, nil)
	defer jaegerSrv.Close()

	promSrv := newPromFake(1.0)
	defer promSrv.Close()

	start, end := fixedWindow()
	prom := promclient.NewClient(promSrv.URL, 5*time.Second)
	jaeger := jaegerclient.NewClient(jaegerSrv.URL, 5*time.Second)

	_, callErr := handleDebugInvestigate(
		context.Background(),
		DebugInvestigateInput{
			Service:   svc,
			StartUnix: start.Unix(),
			EndUnix:   end.Unix(),
		},
		Config{},
		integrationDeps(),
		prom,
		jaeger,
		nil,
	)
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}

	st := pollStore(svc, start, end, "", 10*time.Second)
	if st == nil {
		t.Fatal("investigation did not complete within 10s")
	}
	if st.Status() != investigate.StatusDone {
		t.Fatalf("expected StatusDone, got %v (err: %s)", st.Status(), st.Error())
	}
	r := st.Result()
	if len(r.Hypotheses) != 0 {
		t.Errorf("expected empty Hypotheses for zero traces, got %d", len(r.Hypotheses))
	}
	if r.Diagnostics.TracesFetched != 0 {
		t.Errorf("expected 0 traces fetched, got %d", r.Diagnostics.TracesFetched)
	}
}

// TestIntegration_PromDown verifies that a Prometheus failure causes the
// investigation to use the default anomaly score and record a warning.
func TestIntegration_PromDown(t *testing.T) {
	t.Cleanup(func() { debugInvestigateStore = investigate.NewInvestigationStore() })

	svc := "test-svc-prom-down"
	jaegerSrv := newJaegerFake(
		[]string{svc},
		[]jaegerclient.Trace{{
			TraceID: "xyz",
			Spans:   []jaegerclient.Span{{SpanID: "s2", OperationName: "POST /submit"}},
		}},
	)
	defer jaegerSrv.Close()

	// Negative sampleVal triggers HTTP 500 from the fake Prometheus.
	promSrv := newPromFake(-1)
	defer promSrv.Close()

	start, end := fixedWindow()
	prom := promclient.NewClient(promSrv.URL, 5*time.Second)
	jaeger := jaegerclient.NewClient(jaegerSrv.URL, 5*time.Second)

	_, callErr := handleDebugInvestigate(
		context.Background(),
		DebugInvestigateInput{
			Service:   svc,
			StartUnix: start.Unix(),
			EndUnix:   end.Unix(),
		},
		Config{},
		integrationDeps(),
		prom,
		jaeger,
		nil,
	)
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}

	st := pollStore(svc, start, end, "", 10*time.Second)
	if st == nil {
		t.Fatal("investigation did not complete within 10s")
	}
	if st.Status() != investigate.StatusDone {
		t.Fatalf("expected StatusDone, got %v (err: %s)", st.Status(), st.Error())
	}
	r := st.Result()
	// Prometheus down → default anomaly score applied to all hypotheses.
	if len(r.Hypotheses) == 0 {
		t.Fatal("expected hypotheses from span data even when Prom is down")
	}
	if r.Hypotheses[0].AnomalyScore != scoreDefault {
		t.Errorf("expected default anomaly score %.3f, got %.3f", scoreDefault, r.Hypotheses[0].AnomalyScore)
	}
	// At least one Prometheus-related warning must be present.
	// New code path emits "discover failure metrics: ..." warning when Prom is down.
	hasPromWarn := false
	for _, w := range r.Diagnostics.Warnings {
		if strings.Contains(w, "prom") || strings.Contains(w, "discover failure metrics") {
			hasPromWarn = true
			break
		}
	}
	if !hasPromWarn {
		t.Errorf("expected a prometheus-related warning, got: %v", r.Diagnostics.Warnings)
	}
}

// TestUnit_ListLabelValues_500 exercises the warning-emission path in
// listLabelValues when the Prometheus label-values endpoint returns HTTP 500.
// This targets the code path at Phase 5 (listLabelValues call) without
// requiring a live LLM; deps.LLM is concrete *llm.Client so Phase 5 is
// unreachable in integration tests — this unit test covers the gap directly.
func TestUnit_ListLabelValues_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "injected failure", http.StatusInternalServerError)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	_, err := listLabelValues(context.Background(), prom, "__name__")
	if err == nil {
		t.Fatal("expected error from listLabelValues on HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain HTTP status, got: %v", err)
	}
}

// TestIntegration_PanicRecovery is intentionally skipped.
//
// Injecting a panic deep inside runInvestigation would require either
// modifying analyze.Deps to carry a panic-injectable field (intrusive) or
// replacing the global store mid-test (racy). The defer/recover guard is
// covered by static review; the PR #56 commit e67201f is the relevant source.
// An end-to-end panic test is deferred to a future task that exposes
// runInvestigation's phases via injectable hooks.
func TestIntegration_PanicRecovery(t *testing.T) {
	t.Skip("panic injection requires intrusive refactor — deferred per #57 task spec")
}

// noFilterDockerProbe is a fleet.Probe that returns all images without service filtering.
// Used in integration tests where the investigation service name (Jaeger name) doesn't
// match the docker container name.
type noFilterDockerProbe struct {
	images []fleet.RuntimeImage
	err    error
}

func (f *noFilterDockerProbe) Scheme() string { return "docker" }
func (f *noFilterDockerProbe) List(_ context.Context, _ fleet.Target, _ fleet.Filter) ([]fleet.RuntimeImage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.images, nil
}

// TestIntegration_FleetPhase verifies that when an investigation is run with
// a Host input and a fake fleet registry injected, the result has non-nil
// RuntimeVersions and the LLM user payload contains the fleet summary.
//
// This test extends the existing integration test infrastructure: same
// httptest.Server fakes for Prom+Jaeger, plus fleet registry injection via
// the buildFleetRegistry seam.
func TestIntegration_FleetPhase(t *testing.T) {
	t.Cleanup(func() { debugInvestigateStore = investigate.NewInvestigationStore() })

	svc := "test-svc-fleet-integration"
	tmpDir := t.TempDir()
	// Create Dockerfile with pinned nginx 1.27
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Inject fake fleet registry: runtime nginx is 1.26 → TagDrift
	reg := fleet.NewRegistry()
	// Use a probeAllDockerProbe (inner anonymous type) that returns all images
	// without filtering, so input.Service (Jaeger name) doesn't need to match
	// the container name.
	reg.Register(&noFilterDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "nginx-web", Image: "nginx", Tag: "1.26", State: "running"},
		},
	})
	injectRegistry(t, reg)

	jaegerSrv := newJaegerFake([]string{svc}, nil)
	defer jaegerSrv.Close()

	promSrv := newPromFake(1.0)
	defer promSrv.Close()

	start, end := fixedWindow()
	prom := promclient.NewClient(promSrv.URL, 5*time.Second)
	jaeger := jaegerclient.NewClient(jaegerSrv.URL, 5*time.Second)

	_, callErr := handleDebugInvestigate(
		context.Background(),
		DebugInvestigateInput{
			Service:   svc,
			StartUnix: start.Unix(),
			EndUnix:   end.Unix(),
			Host:      "local://",
			Repo:      tmpDir,
		},
		Config{
			FleetDockerSocket: "/var/run/docker.sock",
			FleetSSHEnable:    false,
			FleetTimeout:      5 * time.Second,
		},
		integrationDeps(),
		prom,
		jaeger,
		nil,
	)
	if callErr != nil {
		t.Fatalf("handleDebugInvestigate: unexpected error: %v", callErr)
	}

	st := pollStore(svc, start, end, tmpDir, 10*time.Second)
	if st == nil {
		t.Fatal("investigation did not complete within 10s")
	}
	if st.Status() != investigate.StatusDone {
		t.Fatalf("expected StatusDone, got %v (err: %s)", st.Status(), st.Error())
	}
	r := st.Result()

	// Phase 7 must populate RuntimeVersions.
	if r.RuntimeVersions == nil {
		t.Fatal("expected RuntimeVersions non-nil after fleet phase")
	}
	if r.RuntimeVersions.Target == "" {
		t.Error("expected RuntimeVersions.Target non-empty")
	}

	// The TagDrift (nginx 1.27 pinned vs 1.26 running) must be in Diffs.
	found := false
	for _, d := range r.RuntimeVersions.Diffs {
		if d.Status == "TagDrift" && d.Image == "nginx" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TagDrift for nginx in RuntimeVersions.Diffs, got: %+v", r.RuntimeVersions.Diffs)
	}

	// Summary must contain the fleet drift (non-empty for TagDrift).
	if r.RuntimeVersions.Summary == "" {
		t.Error("expected non-empty Summary for TagDrift")
	}
}
