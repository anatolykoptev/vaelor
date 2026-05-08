package promclient

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClient_DefaultsTimeout(t *testing.T) {
	c := NewClient("http://localhost:9090", 0)
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default 30s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestNewClient_RespectsCustomTimeout(t *testing.T) {
	c := NewClient("http://localhost:9090", 60*time.Second)
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("expected 60s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestClient_BaseURL_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:9090/", 0)
	if c.baseURL != "http://localhost:9090" {
		t.Errorf("expected trimmed baseURL, got %q", c.baseURL)
	}
}

func TestClient_GetJSON_DecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	var dest struct{ Status string `json:"status"` }
	if err := c.GetJSON(t.Context(), "/test", &dest); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if dest.Status != "success" {
		t.Errorf("got %q, want %q", dest.Status, "success")
	}
}

func TestQueryRange_ParsesMatrixResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "query=up") {
			t.Errorf("missing query=up in %q", r.URL.RawQuery)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{"metric": {"__name__":"up","instance":"a"},"values":[[1700000000,"1"],[1700000060,"0"]]}
				]
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	res, err := c.QueryRange(t.Context(), "up", time.Unix(1700000000, 0), time.Unix(1700000060, 0), 60*time.Second)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(res.Data.Result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.Data.Result))
	}
	if got := res.Data.Result[0].Metric["instance"]; got != "a" {
		t.Errorf("instance label: got %q, want %q", got, "a")
	}
	if len(res.Data.Result[0].Values) != 2 {
		t.Errorf("expected 2 sample points, got %d", len(res.Data.Result[0].Values))
	}
}

func TestQueryRange_EncodesParamsCorrectly(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, _ = c.QueryRange(t.Context(), `rate(http_requests_total{code="500"}[5m])`,
		time.Unix(1700000000, 0), time.Unix(1700000300, 0), 30*time.Second)

	parsed, _ := url.ParseQuery(capturedQuery)
	if got := parsed.Get("query"); got != `rate(http_requests_total{code="500"}[5m])` {
		t.Errorf("query: got %q, want exact match", got)
	}
	if got := parsed.Get("start"); got != "1700000000" {
		t.Errorf("start: got %q", got)
	}
	if got := parsed.Get("end"); got != "1700000300" {
		t.Errorf("end: got %q", got)
	}
	if got := parsed.Get("step"); got != "30" {
		t.Errorf("step: got %q", got)
	}
}

func TestQueryRange_RejectsSubMicrosecondStep(t *testing.T) {
	c := NewClient("http://x", 5*time.Second)
	_, err := c.QueryRange(t.Context(), "up",
		time.Unix(1700000000, 0), time.Unix(1700000060, 0), 100*time.Nanosecond)
	if err == nil {
		t.Error("expected error for sub-microsecond step")
	}
}

func TestQueryRange_AcceptsSubSecondStep(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.QueryRange(t.Context(), "up",
		time.Unix(1700000000, 0), time.Unix(1700000060, 0), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success for 500ms step, got %v", err)
	}
	parsed, _ := url.ParseQuery(captured)
	if got := parsed.Get("step"); got != "0.5" {
		t.Errorf("step encoding: got %q, want %q", got, "0.5")
	}
}

func TestGetJSON_Returns_4xx_With_Body_Preview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	var dest struct{}
	err := c.GetJSON(t.Context(), "/test", &dest)
	if err == nil {
		t.Fatal("expected 4xx error")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "parse error") {
		t.Errorf("error should include status + body preview, got: %v", err)
	}
}
