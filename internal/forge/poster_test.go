package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubPostReview(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/foo/bar/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("want POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"html_url":"https://example/r/1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := newGitHubForgeWithBase("tok", AppConfig{}, srv.URL)
	url, err := g.PostReview(context.Background(), "foo/bar", 42, ReviewPayload{
		Body:  "summary",
		Event: "COMMENT",
		Comments: []InlineComment{
			{Path: "main.go", Line: 10, Body: "nit: unused var"},
		},
	})
	if err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	if !strings.HasPrefix(url, "https://example/") {
		t.Fatalf("want html_url, got %q", url)
	}
	if gotBody["body"] != "summary" {
		t.Fatalf("bad body: %+v", gotBody)
	}
	comments := gotBody["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(comments))
	}
}

func TestGitHubPostIssueComment(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/foo/bar/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("want POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url":"https://example/c/1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	g := newGitHubForgeWithBase("tok", AppConfig{}, srv.URL)
	if _, err := g.PostIssueComment(context.Background(), "foo/bar", 7, "hi"); err != nil {
		t.Fatalf("PostIssueComment: %v", err)
	}
}
