package promclient

import (
	"net/http"
	"net/http/httptest"
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
