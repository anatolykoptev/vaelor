package webanalyze

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnalyze(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		resp := AnalyzeResponse{
			URL:    "https://example.com",
			Status: 200,
			Technologies: []Technology{
				{Name: "React", Category: "JS Framework", Confidence: 100},
			},
			Assets: Assets{
				Scripts:     []string{"app.js"},
				Stylesheets: []string{"style.css"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Analyze(context.Background(), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if len(resp.Technologies) != 1 || resp.Technologies[0].Name != "React" {
		t.Errorf("unexpected technologies: %v", resp.Technologies)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := FetchResponse{Status: 200, Body: "hello"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Fetch(context.Background(), "https://example.com/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != "hello" {
		t.Errorf("expected body 'hello', got %q", resp.Body)
	}
}

func TestAnalyze_Error(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // connection refused
	_, err := c.Analyze(context.Background(), "https://example.com")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}
