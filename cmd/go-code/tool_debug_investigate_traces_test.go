// cmd/go-code/tool_debug_investigate_traces_test.go
//
// Unit tests for runTracesPhase — specifically Phase 2's two-pass fetch
// (error traces + baseline traces) and dedup/merge logic.
//
// Each test calls runTracesPhase directly with a tag-aware httptest fake.
// No integration plumbing (store, polling) is needed here.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
)

// newTagAwareJaegerFake builds an httptest server that differentiates between
// error-tagged and untagged FindTraces requests by inspecting the "tags" query
// param. The client sends tags as JSON (e.g. `{"error":"true"}`), so checking
// for the substring "error" is sufficient.
//
//   - GET /api/services → services
//   - GET /api/traces?tags=...{error... → errorTraces
//   - GET /api/traces (no error tag)  → allTraces
func newTagAwareJaegerFake(
	services []string,
	errorTraces []jaegerclient.Trace,
	allTraces []jaegerclient.Trace,
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/services":
			json.NewEncoder(w).Encode(map[string]any{"data": services})
		case strings.HasPrefix(r.URL.Path, "/api/traces"):
			tags := r.URL.Query().Get("tags")
			if strings.Contains(tags, "error") {
				json.NewEncoder(w).Encode(map[string]any{"data": errorTraces})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"data": allTraces})
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

// newTagAwareJaegerFakeWithError builds a fake where the baseline /api/traces
// call (without error tag) returns an HTTP 500, allowing tests to verify
// graceful degradation.
func newTagAwareJaegerFakeWithError(
	services []string,
	errorTraces []jaegerclient.Trace,
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/services":
			json.NewEncoder(w).Encode(map[string]any{"data": services})
		case strings.HasPrefix(r.URL.Path, "/api/traces"):
			tags := r.URL.Query().Get("tags")
			if strings.Contains(tags, "error") {
				json.NewEncoder(w).Encode(map[string]any{"data": errorTraces})
			} else {
				http.Error(w, "injected baseline failure", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func makeTrace(id string) jaegerclient.Trace {
	return jaegerclient.Trace{
		TraceID: id,
		Spans:   []jaegerclient.Span{{SpanID: "s1", OperationName: "op"}},
	}
}

// TestPhase2_HealthyService_FetchesBaseline verifies that when a service has
// zero error-tagged spans (healthy) the baseline fetch populates TracesFetched
// so Phase 3 symbol correlation is not starved.
func TestPhase2_HealthyService_FetchesBaseline(t *testing.T) {
	svc := "healthy-svc"
	baselineTraces := []jaegerclient.Trace{
		makeTrace("t1"), makeTrace("t2"), makeTrace("t3"),
		makeTrace("t4"), makeTrace("t5"),
	}
	srv := newTagAwareJaegerFake([]string{svc}, nil, baselineTraces)
	defer srv.Close()

	jaeger := jaegerclient.NewClient(srv.URL, 5*time.Second)
	res := &investigate.InvestigationResult{}
	start := time.Unix(1_700_000_000, 0)
	end := time.Unix(1_700_000_600, 0)

	_, traces, err := runTracesPhase(context.Background(), jaeger,
		DebugInvestigateInput{Service: svc}, start, end, res)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if res.Diagnostics.TracesFetched != 5 {
		t.Errorf("TracesFetched = %d, want 5", res.Diagnostics.TracesFetched)
	}
	if len(traces) != 5 {
		t.Errorf("len(traces) = %d, want 5", len(traces))
	}
	if len(res.Diagnostics.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Diagnostics.Warnings)
	}
}

// TestPhase2_FailingService_PrioritizesErrorTraces verifies that error traces
// come first in the result and the total (error + non-error) is reported.
func TestPhase2_FailingService_PrioritizesErrorTraces(t *testing.T) {
	svc := "failing-svc"
	errorTraces := []jaegerclient.Trace{
		makeTrace("err1"), makeTrace("err2"), makeTrace("err3"),
	}
	// allTraces includes the same 3 error traces plus 4 others (total 7),
	// simulating what Jaeger returns without tag filter.
	allTraces := []jaegerclient.Trace{
		makeTrace("err1"), makeTrace("err2"), makeTrace("err3"),
		makeTrace("ok1"), makeTrace("ok2"), makeTrace("ok3"), makeTrace("ok4"),
	}
	srv := newTagAwareJaegerFake([]string{svc}, errorTraces, allTraces)
	defer srv.Close()

	jaeger := jaegerclient.NewClient(srv.URL, 5*time.Second)
	res := &investigate.InvestigationResult{}
	start := time.Unix(1_700_000_000, 0)
	end := time.Unix(1_700_000_600, 0)

	_, traces, err := runTracesPhase(context.Background(), jaeger,
		DebugInvestigateInput{Service: svc}, start, end, res)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	// 3 error + 4 non-overlapping baseline = 7 total
	if res.Diagnostics.TracesFetched != 7 {
		t.Errorf("TracesFetched = %d, want 7", res.Diagnostics.TracesFetched)
	}
	if len(traces) != 7 {
		t.Errorf("len(traces) = %d, want 7", len(traces))
	}
	// Error traces must appear first (highest signal).
	if traces[0].TraceID != "err1" || traces[1].TraceID != "err2" || traces[2].TraceID != "err3" {
		t.Errorf("first 3 traces must be error traces; got %v %v %v",
			traces[0].TraceID, traces[1].TraceID, traces[2].TraceID)
	}
	if len(res.Diagnostics.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Diagnostics.Warnings)
	}
}

// TestPhase2_DedupOnTraceID verifies that a TraceID appearing in both error
// and baseline results is not duplicated in the merged output.
func TestPhase2_DedupOnTraceID(t *testing.T) {
	svc := "dedup-svc"
	sharedTrace := makeTrace("shared")
	errorTraces := []jaegerclient.Trace{sharedTrace}
	// Baseline returns the same trace plus one unique one.
	allTraces := []jaegerclient.Trace{sharedTrace, makeTrace("unique")}

	srv := newTagAwareJaegerFake([]string{svc}, errorTraces, allTraces)
	defer srv.Close()

	jaeger := jaegerclient.NewClient(srv.URL, 5*time.Second)
	res := &investigate.InvestigationResult{}
	start := time.Unix(1_700_000_000, 0)
	end := time.Unix(1_700_000_600, 0)

	_, traces, err := runTracesPhase(context.Background(), jaeger,
		DebugInvestigateInput{Service: svc}, start, end, res)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if len(traces) != 2 {
		t.Errorf("len(traces) = %d, want 2 (no duplicates)", len(traces))
	}
	if res.Diagnostics.TracesFetched != 2 {
		t.Errorf("TracesFetched = %d, want 2", res.Diagnostics.TracesFetched)
	}
	// Shared trace must be first (came from error fetch).
	if traces[0].TraceID != "shared" {
		t.Errorf("first trace must be shared error trace; got %q", traces[0].TraceID)
	}
}

// TestPhase2_BaselineFetchError_StillReturnsErrorTraces verifies that a
// baseline fetch failure degrades gracefully: error traces are still returned,
// TracesFetched reflects that count, and a warning is appended.
func TestPhase2_BaselineFetchError_StillReturnsErrorTraces(t *testing.T) {
	svc := "partial-svc"
	errorTraces := []jaegerclient.Trace{
		makeTrace("e1"), makeTrace("e2"), makeTrace("e3"),
	}
	srv := newTagAwareJaegerFakeWithError([]string{svc}, errorTraces)
	defer srv.Close()

	jaeger := jaegerclient.NewClient(srv.URL, 5*time.Second)
	res := &investigate.InvestigationResult{}
	start := time.Unix(1_700_000_000, 0)
	end := time.Unix(1_700_000_600, 0)

	_, traces, err := runTracesPhase(context.Background(), jaeger,
		DebugInvestigateInput{Service: svc}, start, end, res)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if res.Diagnostics.TracesFetched != 3 {
		t.Errorf("TracesFetched = %d, want 3", res.Diagnostics.TracesFetched)
	}
	if len(traces) != 3 {
		t.Errorf("len(traces) = %d, want 3", len(traces))
	}
	// A warning about the baseline failure must be recorded.
	found := false
	for _, w := range res.Diagnostics.Warnings {
		if strings.Contains(w, "baseline") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a warning containing %q; warnings: %v", "baseline", res.Diagnostics.Warnings)
	}
}
