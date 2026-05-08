package promclient

import (
	"net/http"
	"net/http/httptest"
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
	if err := c.getJSON(t.Context(), "/test", &dest); err != nil {
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

	for _, want := range []string{"query=rate", "start=1700000000", "end=1700000300", "step=30"} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("missing %q in query string %q", want, capturedQuery)
		}
	}
}
