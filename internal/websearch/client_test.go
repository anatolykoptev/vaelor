package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		out := searchOutput{
			Query: "test",
			Sources: []sourceItem{
				{Title: "repo1", URL: "https://github.com/foo/bar"},
				{Title: "repo2", URL: "https://github.com/baz/qux"},
			},
		}
		text, _ := json.Marshal(out)
		resp := mcpResponse{}
		resp.Result.Content = []mcpContent{{Type: "text", Text: string(text)}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	results, err := client.Search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].URL != "https://github.com/foo/bar" {
		t.Errorf("unexpected URL: %s", results[0].URL)
	}
	if results[1].Title != "repo2" {
		t.Errorf("unexpected title: %s", results[1].Title)
	}
}

func TestSearchHandlesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "test")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSearchHandlesMCPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"result": nil,
			"error":  map[string]string{"message": "tool not found"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "test")
	if err == nil {
		t.Error("expected error for MCP error response")
	}
}

func TestSearchNilClient(t *testing.T) {
	var client *Client
	if client != nil {
		t.Error("expected nil client")
	}
}

func TestSearchEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out := searchOutput{Query: "empty"}
		text, _ := json.Marshal(out)
		resp := mcpResponse{}
		resp.Result.Content = []mcpContent{{Type: "text", Text: string(text)}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	results, err := client.Search(context.Background(), "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
