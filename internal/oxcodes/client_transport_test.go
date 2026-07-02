package oxcodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientSearch_RequestShapeAndDecode proves the httputil-backed transport
// preserves the wire contract: POST to the right path with the marshaled
// request body, and the response gets decoded back into SearchResponse.
// RED guarantee: break the path/method passed to httputil.PostJSON (or drop
// the decode), and this test fails.
func TestClientSearch_RequestShapeAndDecode(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody SearchInput

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		resp := SearchResponse{
			Matches:      []SearchMatch{{File: "a.go", Line: 1, Text: "func foo()"}},
			TotalMatches: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	result, err := client.Search(context.Background(), SearchInput{
		Root:    "/repo",
		Pattern: "foo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/search" {
		t.Errorf("expected /search, got %s", gotPath)
	}
	if gotBody.Root != "/repo" || gotBody.Pattern != "foo" {
		t.Errorf("unexpected request body: %+v", gotBody)
	}
	if result.TotalMatches != 1 || len(result.Matches) != 1 || result.Matches[0].File != "a.go" {
		t.Errorf("unexpected decoded result: %+v", result)
	}
}

// TestClientSearch_ErrorStatus proves a non-2xx status still surfaces as a
// non-nil error through the httputil-backed transport.
func TestClientSearch_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), SearchInput{Root: "/repo", Pattern: "foo"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestClientRewrite_RequestShapeAndDecode covers the Rewrite path, which
// unlike Search/SearchScoped/SearchStructural decodes into a distinct
// response type via the shared doPost helper.
func TestClientRewrite_RequestShapeAndDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rewrite" {
			t.Errorf("expected /rewrite, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		resp := RewriteResponse{TotalMatches: 2, TotalFiles: 1}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	result, err := client.Rewrite(context.Background(), RewriteInput{
		Root: "/repo", Pattern: "foo", Rewrite: "bar", Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalMatches != 2 || result.TotalFiles != 1 {
		t.Errorf("unexpected decoded result: %+v", result)
	}
}
