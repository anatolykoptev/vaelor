package jaegerclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient_DefaultsTimeout(t *testing.T) {
	c := NewClient("http://localhost:16686", 0)
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default 30s, got %v", c.httpClient.Timeout)
	}
}

func TestListServices_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			t.Errorf("expected /api/services, got %q", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":["go-code","oxpulse-chat","memdb-go"],"total":3}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	got, err := c.ListServices(t.Context())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(got) != 3 || got[0] != "go-code" {
		t.Errorf("unexpected services: %v", got)
	}
}

func TestListServices_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":null,"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	got, err := c.ListServices(t.Context())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestClient_BaseURL_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:16686/", 0)
	if !strings.HasSuffix(c.baseURL, "16686") {
		t.Errorf("expected trimmed, got %q", c.baseURL)
	}
}

func TestFindTraces_BuildsCorrectQuery(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.FindTraces(t.Context(), FindTracesParams{
		Service:   "go-code",
		Tags:      map[string]string{"error": "true"},
		StartTime: time.Unix(1700000000, 0),
		EndTime:   time.Unix(1700001000, 0),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("FindTraces: %v", err)
	}
	for _, want := range []string{"service=go-code", "limit=10", "start=1700000000000000", "end=1700001000000000"} {
		if !strings.Contains(captured, want) {
			t.Errorf("missing %q in query %q", want, captured)
		}
	}
}

func TestFindTraces_DecodesSpansAndOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"traceID": "abc123",
					"spans": [
						{"spanID":"s1","operationName":"/api.Service/Method","duration":50000,
						 "tags":[{"key":"error","type":"bool","value":true}]}
					],
					"processes": {"p1": {"serviceName": "go-code"}}
				}
			],
			"total": 1
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	traces, err := c.FindTraces(t.Context(), FindTracesParams{Service: "go-code", Limit: 1})
	if err != nil {
		t.Fatalf("FindTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].TraceID != "abc123" {
		t.Errorf("traceID: got %q", traces[0].TraceID)
	}
	if len(traces[0].Spans) != 1 || traces[0].Spans[0].OperationName != "/api.Service/Method" {
		t.Errorf("operation: got %+v", traces[0].Spans)
	}
}

func TestGetTrace_FetchesByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces/abc123" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"traceID":"abc123","spans":[]}],"total":1}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	tr, err := c.GetTrace(t.Context(), "abc123")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if tr.TraceID != "abc123" {
		t.Errorf("traceID: got %q", tr.TraceID)
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.GetTrace(t.Context(), "nonexistent")
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}
