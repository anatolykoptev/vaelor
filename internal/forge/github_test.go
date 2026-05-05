package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestGitHubForge(t *testing.T, mux *http.ServeMux) *GitHubForge {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newGitHubForgeWithBase("test-token", AppConfig{}, srv.URL)
}

func TestGitHubForgeKind(t *testing.T) {
	g := NewGitHubForge("", AppConfig{})
	if got := g.Kind(); got != GitHub {
		t.Errorf("Kind() = %v, want GitHub", got)
	}
}

func TestGitHubFetchRepoMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/foo/bar", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ghHeaderAPIVersion) != ghAPIVersion {
			http.Error(w, "missing api version header", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name":      "foo/bar",
			"description":    "test repo",
			"default_branch": "main",
			"language":       "Go",
			"stargazers_count": 42,
			"forks_count":    7,
			"clone_url":      "https://github.com/foo/bar.git",
			"private":        false,
			"size":           1024,
		})
	})

	g := newTestGitHubForge(t, mux)

	t.Run("plain slug", func(t *testing.T) {
		meta, err := g.FetchRepoMeta(context.Background(), "foo/bar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.FullName != "foo/bar" {
			t.Errorf("FullName = %q, want %q", meta.FullName, "foo/bar")
		}
		if meta.Stars != 42 {
			t.Errorf("Stars = %d, want 42", meta.Stars)
		}
		if meta.Language != "Go" {
			t.Errorf("Language = %q, want Go", meta.Language)
		}
		if meta.DefaultBranch != "main" {
			t.Errorf("DefaultBranch = %q, want main", meta.DefaultBranch)
		}
		if meta.Forks != 7 {
			t.Errorf("Forks = %d, want 7", meta.Forks)
		}
	})

	t.Run("full URL stripped", func(t *testing.T) {
		meta, err := g.FetchRepoMeta(context.Background(), "https://github.com/foo/bar.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.FullName != "foo/bar" {
			t.Errorf("FullName = %q, want foo/bar", meta.FullName)
		}
	})
}

func TestGitHubFetchRepoMeta_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/foo/missing", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	g := newTestGitHubForge(t, mux)
	_, err := g.FetchRepoMeta(context.Background(), "foo/missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGitHubFetchREADME(t *testing.T) {
	const readmeContent = "# Hello\nThis is a readme."

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/foo/bar/readme", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ghHeaderAccept) != ghMediaTypeRaw {
			http.Error(w, "wrong accept header", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(readmeContent))
	})
	mux.HandleFunc("GET /repos/foo/noreadme/readme", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	g := newTestGitHubForge(t, mux)

	t.Run("found", func(t *testing.T) {
		got, err := g.FetchREADME(context.Background(), "foo/bar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != readmeContent {
			t.Errorf("README = %q, want %q", got, readmeContent)
		}
	})

	t.Run("not found returns empty string", func(t *testing.T) {
		got, err := g.FetchREADME(context.Background(), "foo/noreadme")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("README = %q, want empty string", got)
		}
	})
}

func TestGitHubSearchCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ghHeaderAccept) != ghMediaTypeText {
			http.Error(w, "wrong accept", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
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
		})
	})

	g := newTestGitHubForge(t, mux)
	results, err := g.SearchCode(context.Background(), "func main", []string{"foo/bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.Name != "main.go" {
		t.Errorf("Name = %q, want main.go", r.Name)
	}
	if r.Path != "cmd/main.go" {
		t.Errorf("Path = %q, want cmd/main.go", r.Path)
	}
	if r.Repo != "foo/bar" {
		t.Errorf("Repo = %q, want foo/bar", r.Repo)
	}
	wantContent := "func main() {\n---\nos.Exit(1)"
	if r.Content != wantContent {
		t.Errorf("Content = %q, want %q", r.Content, wantContent)
	}
}

func TestGitHubSearchRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/repositories", func(w http.ResponseWriter, r *http.Request) {
		sort := r.URL.Query().Get("sort")
		if sort != "stars" {
			http.Error(w, "expected sort=stars", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"full_name":        "foo/bar",
					"description":      "A great repo",
					"stargazers_count": 999,
					"language":         "Go",
					"topics":           []string{"golang", "cli"},
					"pushed_at":        "2024-01-15T10:00:00Z",
					"archived":         false,
					"html_url":         "https://github.com/foo/bar",
				},
			},
		})
	})

	g := newTestGitHubForge(t, mux)
	results, err := g.SearchRepos(context.Background(), "golang cli", "stars")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.FullName != "foo/bar" {
		t.Errorf("FullName = %q, want foo/bar", r.FullName)
	}
	if r.Stars != 999 {
		t.Errorf("Stars = %d, want 999", r.Stars)
	}
	if r.Language != "Go" {
		t.Errorf("Language = %q, want Go", r.Language)
	}
	if len(r.Topics) != 2 || r.Topics[0] != "golang" {
		t.Errorf("Topics = %v, want [golang cli]", r.Topics)
	}
	if r.Archived {
		t.Error("Archived = true, want false")
	}
}

func TestGitHubSearchIssues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/issues", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"number":   42,
					"title":    "Fix the bug",
					"html_url": "https://github.com/foo/bar/issues/42",
					"state":    "closed",
					"user":     map[string]any{"login": "alice"},
					"labels": []map[string]any{
						{"name": "bug"},
						{"name": "good first issue"},
					},
					"body":       "Please fix this.",
					"comments":   3,
					"created_at": "2024-01-01T00:00:00Z",
					"repository": map[string]any{"full_name": "foo/bar"},
					"pull_request": map[string]any{
						"merged_at": "2024-01-10T12:00:00Z",
					},
				},
				{
					"number":     7,
					"title":      "Regular issue",
					"html_url":   "https://github.com/foo/bar/issues/7",
					"state":      "open",
					"user":       map[string]any{"login": "bob"},
					"labels":     []map[string]any{},
					"body":       "",
					"comments":   0,
					"created_at": "2024-02-01T00:00:00Z",
					"repository": map[string]any{"full_name": "foo/bar"},
					// no pull_request field
				},
			},
		})
	})

	g := newTestGitHubForge(t, mux)
	results, err := g.SearchIssues(context.Background(), "bug repo:foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	pr := results[0]
	if pr.Number != 42 {
		t.Errorf("Number = %d, want 42", pr.Number)
	}
	if pr.Author != "alice" {
		t.Errorf("Author = %q, want alice", pr.Author)
	}
	if len(pr.Labels) != 2 || pr.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug, good first issue]", pr.Labels)
	}
	if pr.MergedAt != "2024-01-10T12:00:00Z" {
		t.Errorf("MergedAt = %q, want 2024-01-10T12:00:00Z", pr.MergedAt)
	}
	if pr.Repo != "foo/bar" {
		t.Errorf("Repo = %q, want foo/bar", pr.Repo)
	}

	issue := results[1]
	if issue.MergedAt != "" {
		t.Errorf("MergedAt = %q, want empty for plain issue", issue.MergedAt)
	}
	if issue.Author != "bob" {
		t.Errorf("Author = %q, want bob", issue.Author)
	}
}

func TestGitHubSearchIssues_UnprocessableEntity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/issues", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unprocessable", http.StatusUnprocessableEntity)
	})

	g := newTestGitHubForge(t, mux)
	_, err := g.SearchIssues(context.Background(), "bad query")
	if err == nil {
		t.Fatal("expected error for 422, got nil")
	}
}
