package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/foo/bar", "foo", "bar", true},
		{"https://github.com/foo/bar.git", "foo", "bar", true},
		{"https://github.com/topics/golang", "topics", "golang", true},
		{"https://github.com/foo", "", "", false},
		{"https://example.com/foo/bar", "", "", false},
		{"not-a-url", "", "", false},
		{"https://github.com/", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			owner, repo, ok := ExtractOwnerRepo(tc.url)
			if ok != tc.ok {
				t.Errorf("ok: got %v, want %v", ok, tc.ok)
			}
			if owner != tc.owner {
				t.Errorf("owner: got %q, want %q", owner, tc.owner)
			}
			if repo != tc.repo {
				t.Errorf("repo: got %q, want %q", repo, tc.repo)
			}
		})
	}
}

func TestSearchCode(t *testing.T) {
	payload := map[string]any{
		"items": []map[string]any{
			{
				"name":     "main.go",
				"path":     "cmd/main.go",
				"html_url": "https://github.com/foo/bar/blob/main/cmd/main.go",
				"repository": map[string]any{
					"full_name": "foo/bar",
				},
				"text_matches": []map[string]any{
					{"fragment": "func main() {"},
					{"fragment": "os.Exit(1)"},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/code" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL

	results, err := c.SearchCode(context.Background(), "func main", []string{"foo/bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.Name != "main.go" {
		t.Errorf("Name: got %q, want %q", got.Name, "main.go")
	}
	if got.Path != "cmd/main.go" {
		t.Errorf("Path: got %q, want %q", got.Path, "cmd/main.go")
	}
	if got.Repo != "foo/bar" {
		t.Errorf("Repo: got %q, want %q", got.Repo, "foo/bar")
	}
	wantContent := "func main() {\n---\nos.Exit(1)"
	if got.Content != wantContent {
		t.Errorf("Content: got %q, want %q", got.Content, wantContent)
	}
}

func TestSearchIssues(t *testing.T) {
	payload := map[string]any{
		"items": []map[string]any{
			{
				"number":     42,
				"title":      "Fix nil pointer in handler",
				"html_url":   "https://github.com/foo/bar/issues/42",
				"state":      "open",
				"user":       map[string]any{"login": "alice"},
				"labels":     []map[string]any{{"name": "bug"}, {"name": "good first issue"}},
				"body":       "Steps to reproduce...",
				"comments":   3,
				"created_at": "2024-01-15T10:00:00Z",
				"repository": map[string]any{"full_name": "foo/bar"},
				"pull_request": map[string]any{"merged_at": nil},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL

	results, err := c.SearchIssues(context.Background(), "nil pointer repo:foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.Number != 42 {
		t.Errorf("Number: got %d, want 42", got.Number)
	}
	if got.Title != "Fix nil pointer in handler" {
		t.Errorf("Title: got %q", got.Title)
	}
	if got.Author != "alice" {
		t.Errorf("Author: got %q, want %q", got.Author, "alice")
	}
	if len(got.Labels) != 2 || got.Labels[0] != "bug" {
		t.Errorf("Labels: got %v", got.Labels)
	}
	if got.Comments != 3 {
		t.Errorf("Comments: got %d, want 3", got.Comments)
	}
	if got.Repo != "foo/bar" {
		t.Errorf("Repo: got %q, want %q", got.Repo, "foo/bar")
	}
}

func TestSearchRepos(t *testing.T) {
	payload := map[string]any{
		"items": []map[string]any{
			{
				"full_name":        "golang/go",
				"description":      "The Go programming language",
				"stargazers_count": 120000,
				"language":         "Go",
				"topics":           []string{"go", "programming-language"},
				"pushed_at":        "2024-06-01T12:00:00Z",
				"archived":         false,
				"html_url":         "https://github.com/golang/go",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/repositories" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := NewClient("")
	c.apiBase = srv.URL

	results, err := c.SearchRepos(context.Background(), "language:go mcp server", "stars")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.FullName != "golang/go" {
		t.Errorf("FullName: got %q, want %q", got.FullName, "golang/go")
	}
	if got.Stars != 120000 {
		t.Errorf("Stars: got %d, want 120000", got.Stars)
	}
	if got.Language != "Go" {
		t.Errorf("Language: got %q, want %q", got.Language, "Go")
	}
	if len(got.Topics) != 2 || got.Topics[0] != "go" {
		t.Errorf("Topics: got %v", got.Topics)
	}
	if got.Archived {
		t.Error("Archived: expected false")
	}
}
