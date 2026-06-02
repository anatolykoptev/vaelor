package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestGitLabForge(t *testing.T, mux *http.ServeMux) *GitLabForge {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newGitLabForgeWithBase("gl-test-token", srv.URL)
}

func TestGitLabForgeKind(t *testing.T) {
	g := NewGitLabForge("", "")
	if got := g.Kind(); got != GitLab {
		t.Errorf("Kind() = %v, want GitLab", got)
	}
}

func TestGitLabDefaultAPIBase(t *testing.T) {
	g := NewGitLabForge("", "")
	if g.apiBase != glDefaultAPIBase {
		t.Errorf("apiBase = %q, want %q", g.apiBase, glDefaultAPIBase)
	}
}

func TestGitLabFetchRepoMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "group/repo",
			"description":         "A test project",
			"default_branch":      "main",
			"star_count":          55,
			"forks_count":         3,
			"http_url_to_repo":    "https://gitlab.com/group/repo.git",
			"visibility":          "public",
		})
	})

	g := newTestGitLabForge(t, mux)

	t.Run("plain slug", func(t *testing.T) {
		meta, err := g.FetchRepoMeta(context.Background(), "group/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.FullName != "group/repo" {
			t.Errorf("FullName = %q, want %q", meta.FullName, "group/repo")
		}
		if meta.Stars != 55 {
			t.Errorf("Stars = %d, want 55", meta.Stars)
		}
		if meta.Forks != 3 {
			t.Errorf("Forks = %d, want 3", meta.Forks)
		}
		if meta.DefaultBranch != "main" {
			t.Errorf("DefaultBranch = %q, want main", meta.DefaultBranch)
		}
		if meta.Private {
			t.Error("Private = true, want false for public repo")
		}
		if meta.CloneURL != "https://gitlab.com/group/repo.git" {
			t.Errorf("CloneURL = %q", meta.CloneURL)
		}
	})

	t.Run("private visibility", func(t *testing.T) {
		mux2 := http.NewServeMux()
		mux2.HandleFunc("GET /api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path_with_namespace": "group/repo",
				"visibility":          "private",
			})
		})
		g2 := newTestGitLabForge(t, mux2)
		meta, err := g2.FetchRepoMeta(context.Background(), "group/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !meta.Private {
			t.Error("Private = false, want true for private repo")
		}
	})
}

func TestGitLabFetchREADME(t *testing.T) {
	const content = "# GitLab Readme\nHello world."

	mux := http.NewServeMux()
	// Project metadata endpoint (used to get default branch).
	mux.HandleFunc("GET /api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "group/repo",
			"default_branch":      "main",
			"visibility":          "public",
		})
	})
	// README raw endpoint.
	mux.HandleFunc("GET /api/v4/projects/group%2Frepo/repository/files/README.md/raw", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "main" {
			http.Error(w, "wrong ref", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(content))
	})

	// Repo with no README.
	mux.HandleFunc("GET /api/v4/projects/group%2Fnodocs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "group/nodocs",
			"default_branch":      "main",
			"visibility":          "public",
		})
	})
	mux.HandleFunc("GET /api/v4/projects/group%2Fnodocs/repository/files/README.md/raw", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	g := newTestGitLabForge(t, mux)

	t.Run("found", func(t *testing.T) {
		got, err := g.FetchREADME(context.Background(), "group/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content {
			t.Errorf("README = %q, want %q", got, content)
		}
	})

	t.Run("not found returns empty string", func(t *testing.T) {
		got, err := g.FetchREADME(context.Background(), "group/nodocs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("README = %q, want empty string", got)
		}
	})
}

func TestGitLabSearchCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/owner%2Frepo/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("scope") != "blobs" {
			http.Error(w, "wrong scope", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"filename": "main.go",
				"path":     "cmd/main.go",
				"data":     "func main() {\n\tos.Exit(0)\n}",
				"ref":      "main",
			},
		})
	})

	g := newTestGitLabForge(t, mux)
	results, err := g.SearchCode(context.Background(), "func main", []string{"owner/repo"})
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
	if r.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want owner/repo", r.Repo)
	}
}

func TestGitLabSearchCode_EmptyRepos(t *testing.T) {
	g := newGitLabForgeWithBase("", "http://unused")
	results, err := g.SearchCode(context.Background(), "anything", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestGitLabSearchRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("order_by") != "star_count" {
			http.Error(w, "expected order_by=star_count", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"path_with_namespace": "mygroup/myrepo",
				"description":         "My great project",
				"star_count":          120,
				"last_activity_at":    "2024-03-01T12:00:00Z",
				"archived":            false,
				"web_url":             "https://gitlab.com/mygroup/myrepo",
			},
		})
	})

	g := newTestGitLabForge(t, mux)
	results, err := g.SearchRepos(context.Background(), "myrepo", "stars")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	r := results[0]
	if r.FullName != "mygroup/myrepo" {
		t.Errorf("FullName = %q, want mygroup/myrepo", r.FullName)
	}
	if r.Stars != 120 {
		t.Errorf("Stars = %d, want 120", r.Stars)
	}
	if r.Archived {
		t.Error("Archived = true, want false")
	}
	if r.HTMLURL != "https://gitlab.com/mygroup/myrepo" {
		t.Errorf("HTMLURL = %q", r.HTMLURL)
	}
}

func TestGitLabSearchIssues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/owner%2Frepo/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("scope") != "issues" {
			http.Error(w, "wrong scope", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"iid":     7,
				"title":   "Fix nil pointer",
				"web_url": "https://gitlab.com/owner/repo/-/issues/7",
				"state":   "opened",
				"author":  map[string]any{"username": "alice"},
				"labels": []map[string]any{
					{"title": "bug"},
				},
				"description":      "This crashes on startup.",
				"user_notes_count": 2,
				"created_at":       "2024-05-01T00:00:00Z",
			},
		})
	})

	g := newTestGitLabForge(t, mux)
	results, err := g.SearchIssues(context.Background(), "nil pointer repo:owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	item := results[0]
	if item.Number != 7 {
		t.Errorf("Number = %d, want 7", item.Number)
	}
	if item.Author != "alice" {
		t.Errorf("Author = %q, want alice", item.Author)
	}
	if len(item.Labels) != 1 || item.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", item.Labels)
	}
	if item.Comments != 2 {
		t.Errorf("Comments = %d, want 2", item.Comments)
	}
	if item.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want owner/repo", item.Repo)
	}
}

func TestGitLabSearchIssues_NoRepo(t *testing.T) {
	g := newGitLabForgeWithBase("", "http://unused")
	results, err := g.SearchIssues(context.Background(), "some query without repo token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestGitLabAuthHeader(t *testing.T) {
	var gotToken string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/a%2Fb", func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(glHeaderPrivateToken)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "a/b",
			"visibility":          "public",
		})
	})

	g := newTestGitLabForge(t, mux)
	_, err := g.FetchRepoMeta(context.Background(), "a/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotToken != "gl-test-token" {
		t.Errorf("PRIVATE-TOKEN = %q, want gl-test-token", gotToken)
	}
}
