# Forge Abstraction — Multi-Forge Support (GitHub + GitLab)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Abstract the hardcoded GitHub dependency into a `Forge` interface so go-code can work with GitHub, GitLab, and future forges.

**Architecture:** Create `internal/forge/` package with a `Forge` interface and shared types. Move existing GitHub code into `forge.GitHub` implementation. Add `forge.GitLab` implementation using GitLab REST API v4. Update `ingest/clone.go` to detect forge from URL and build correct clone URLs. Update `analyze.Deps` and all tool handlers to use `forge.Forge` instead of `*github.Client`.

**Tech Stack:** Go, `net/http`, GitLab REST API v4, `httptest` for mocking

---

## Context for Implementers

### Current Architecture
- `internal/github/` — GitHub-specific client (3 files: `github.go`, `search.go`, `search_test.go`)
- `internal/ingest/clone.go` — hardcoded `github.com` regex + clone URL building
- `internal/analyze/analyze.go:67` — `GitHub *github.Client` in `Deps` struct
- `cmd/go-code/register.go:54` — `github.NewClient(cfg.GithubToken)` wired into `deps.GitHub`
- `cmd/go-code/tool_repo_analyze.go` — uses `deps.GitHub.SearchCode/SearchIssues/FetchRepoMeta/FetchREADME`
- `cmd/go-code/tool_repo_search.go` — uses `deps.GitHub.SearchRepos/FetchRepoMeta/FetchREADME` + `github.ExtractOwnerRepo`
- `cmd/go-code/resolve.go` — uses `ingest.IsRemote/NormalizeSlug/CloneRepo`

### Key Differences GitHub vs GitLab
| Feature | GitHub | GitLab |
|---------|--------|--------|
| Slug format | `owner/repo` (2 parts) | `group/subgroup/.../repo` (N parts) |
| API base | `https://api.github.com` | `https://gitlab.com/api/v4` |
| Auth header | `Authorization: Bearer {token}` | `PRIVATE-TOKEN: {token}` |
| Repo meta | `GET /repos/{slug}` | `GET /projects/{url-encoded-path}` |
| README | `GET /repos/{slug}/readme` | `GET /projects/{id}/repository/files/README.md/raw?ref=main` |
| Code search | `GET /search/code?q=...` | `GET /projects/{id}/search?scope=blobs&search=...` |
| Issues search | `GET /search/issues?q=...` | `GET /projects/{id}/issues?search=...` |
| Repo search | `GET /search/repositories?q=...` | `GET /projects?search=...&order_by=stars` |
| Clone URL | `https://github.com/{slug}.git` | `https://gitlab.com/{slug}.git` |

### Config Changes Needed
- `GITLAB_TOKEN` — env var for GitLab API token
- `GITLAB_URL` — optional, defaults to `https://gitlab.com` (for self-hosted)

---

### Task 1: Create `forge` Package — Interface + Shared Types

Extract types from `internal/github/` into new `internal/forge/` package with the `Forge` interface.

**Files:**
- Create: `internal/forge/forge.go`
- Test: `internal/forge/forge_test.go`

**Step 1: Write the test**

```go
// internal/forge/forge_test.go
package forge

import "testing"

func TestForgeKindString(t *testing.T) {
	tests := []struct {
		kind ForgeKind
		want string
	}{
		{GitHub, "github"},
		{GitLab, "gitlab"},
		{Unknown, "unknown"},
		{ForgeKind(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ForgeKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -run TestForgeKindString -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Create the forge package**

```go
// internal/forge/forge.go
package forge

import "context"

// ForgeKind identifies a code forge provider.
type ForgeKind int

const (
	Unknown ForgeKind = iota
	GitHub
	GitLab
)

func (k ForgeKind) String() string {
	switch k {
	case GitHub:
		return "github"
	case GitLab:
		return "gitlab"
	default:
		return "unknown"
	}
}

// RepoMeta contains key metadata about a repository.
type RepoMeta struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	Language      string `json:"language"`
	Stars         int    `json:"stargazers_count"`
	Forks         int    `json:"forks_count"`
	CloneURL      string `json:"clone_url"`
	Private       bool   `json:"private"`
	Size          int    `json:"size"`
}

// CodeResult represents a code search result.
type CodeResult struct {
	Name    string
	Path    string
	URL     string
	Repo    string
	Content string
}

// IssueItem represents an issue or merge/pull request.
type IssueItem struct {
	Number    int
	Title     string
	URL       string
	State     string
	Author    string
	Labels    []string
	Body      string
	Comments  int
	CreatedAt string
	MergedAt  string
	Repo      string
}

// RepoSearchResult represents a repository from search results.
type RepoSearchResult struct {
	FullName    string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	HTMLURL     string
}

// Forge is the interface for interacting with a code forge (GitHub, GitLab, etc.).
type Forge interface {
	// Kind returns the forge provider type.
	Kind() ForgeKind

	// FetchRepoMeta fetches repository metadata.
	FetchRepoMeta(ctx context.Context, slug string) (*RepoMeta, error)

	// FetchREADME fetches the raw README content. Returns "" if not found.
	FetchREADME(ctx context.Context, slug string) (string, error)

	// SearchCode searches code within repositories.
	SearchCode(ctx context.Context, query string, repos []string) ([]CodeResult, error)

	// SearchIssues searches issues and pull/merge requests.
	SearchIssues(ctx context.Context, query string) ([]IssueItem, error)

	// SearchRepos searches for repositories.
	SearchRepos(ctx context.Context, query, sort string) ([]RepoSearchResult, error)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -run TestForgeKindString -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/forge/
git commit -m "feat(forge): add Forge interface and shared types"
```

---

### Task 2: URL Detection — `DetectForge` and `ExtractSlug`

Add functions to detect forge type from URL/slug and extract the normalized slug.

**Files:**
- Create: `internal/forge/detect.go`
- Create: `internal/forge/detect_test.go`

**Step 1: Write the tests**

```go
// internal/forge/detect_test.go
package forge

import "testing"

func TestDetectForge(t *testing.T) {
	tests := []struct {
		input string
		kind  ForgeKind
	}{
		// GitHub
		{"https://github.com/foo/bar", GitHub},
		{"https://github.com/foo/bar.git", GitHub},
		{"foo/bar", GitHub}, // bare slug defaults to GitHub
		// GitLab
		{"https://gitlab.com/foo/bar", GitLab},
		{"https://gitlab.com/group/sub/repo", GitLab},
		{"https://gitlab.com/group/sub/repo.git", GitLab},
		{"https://my-gitlab.company.com/foo/bar", Unknown}, // custom domains need explicit config
		// Local paths
		{"/home/user/src/project", Unknown},
		{"./relative/path", Unknown},
		{"../parent/path", Unknown},
		// Invalid
		{"", Unknown},
		{"https://bitbucket.org/foo/bar", Unknown},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := DetectForge(tc.input); got != tc.kind {
				t.Errorf("DetectForge(%q) = %v, want %v", tc.input, got, tc.kind)
			}
		})
	}
}

func TestExtractSlug(t *testing.T) {
	tests := []struct {
		input string
		slug  string
		ok    bool
	}{
		// GitHub
		{"https://github.com/foo/bar", "foo/bar", true},
		{"https://github.com/foo/bar.git", "foo/bar", true},
		{"foo/bar", "foo/bar", true},
		// GitLab
		{"https://gitlab.com/foo/bar", "foo/bar", true},
		{"https://gitlab.com/group/sub/repo", "group/sub/repo", true},
		{"https://gitlab.com/group/sub/repo.git", "group/sub/repo", true},
		// Invalid
		{"https://github.com/foo", "", false},
		{"/local/path", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			slug, ok := ExtractSlug(tc.input)
			if ok != tc.ok {
				t.Errorf("ExtractSlug(%q) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if slug != tc.slug {
				t.Errorf("ExtractSlug(%q) slug = %q, want %q", tc.input, slug, tc.slug)
			}
		})
	}
}

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/foo/bar", "foo", "bar", true},
		{"https://github.com/foo/bar.git", "foo", "bar", true},
		{"https://gitlab.com/foo/bar", "foo", "bar", true},
		{"https://gitlab.com/group/sub/repo", "group/sub", "repo", true},
		{"https://github.com/foo", "", "", false},
		{"https://example.com/foo/bar", "", "", false},
		{"not-a-url", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			owner, repo, ok := ExtractOwnerRepo(tc.url)
			if ok != tc.ok || owner != tc.owner || repo != tc.repo {
				t.Errorf("ExtractOwnerRepo(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.url, owner, repo, ok, tc.owner, tc.repo, tc.ok)
			}
		})
	}
}

func TestIsRemote(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"foo/bar", true},
		{"https://github.com/foo/bar", true},
		{"https://gitlab.com/group/sub/repo", true},
		{"/home/user/project", false},
		{"./relative", false},
		{"../parent", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := IsRemote(tc.input); got != tc.want {
				t.Errorf("IsRemote(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCloneURL(t *testing.T) {
	tests := []struct {
		kind  ForgeKind
		slug  string
		host  string
		token string
		want  string
	}{
		{GitHub, "foo/bar", "", "", "https://github.com/foo/bar.git"},
		{GitHub, "foo/bar", "", "tok123", "https://tok123@github.com/foo/bar.git"},
		{GitLab, "group/repo", "", "", "https://gitlab.com/group/repo.git"},
		{GitLab, "group/repo", "https://my-gl.co", "", "https://my-gl.co/group/repo.git"},
		{GitLab, "group/repo", "https://my-gl.co", "glpat-xxx", "https://oauth2:glpat-xxx@my-gl.co/group/repo.git"},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			got := CloneURL(tc.kind, tc.slug, tc.host, tc.token)
			if got != tc.want {
				t.Errorf("CloneURL(%v, %q, %q, %q) = %q, want %q",
					tc.kind, tc.slug, tc.host, tc.token, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -v`
Expected: FAIL — functions don't exist yet

**Step 3: Implement detect.go**

```go
// internal/forge/detect.go
package forge

import (
	"net/url"
	"strings"
)

// DetectForge identifies the forge provider from a URL or slug.
func DetectForge(input string) ForgeKind {
	if input == "" {
		return Unknown
	}
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return Unknown
	}
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		u, err := url.Parse(input)
		if err != nil {
			return Unknown
		}
		switch u.Hostname() {
		case "github.com":
			return GitHub
		case "gitlab.com":
			return GitLab
		default:
			return Unknown
		}
	}
	// Bare slug like "owner/repo" — default to GitHub.
	parts := strings.Split(input, "/")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return GitHub
	}
	return Unknown
}

// ExtractSlug extracts the normalized slug from a URL or bare slug.
// For GitHub: "owner/repo". For GitLab: "group/sub/.../repo" (N levels).
func ExtractSlug(input string) (string, bool) {
	if input == "" {
		return "", false
	}
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return "", false
	}
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", false
		}
		path := strings.Trim(u.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", false
		}
		return path, true
	}
	// Bare slug.
	slug := strings.TrimSuffix(input, ".git")
	parts := strings.Split(slug, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return slug, true
}

// ExtractOwnerRepo parses a forge URL into owner (or group path) and repo name.
// For GitLab with nested groups: owner="group/sub", repo="repo".
func ExtractOwnerRepo(rawURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}
	host := u.Hostname()
	if host != "github.com" && host != "gitlab.com" {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	repoName := parts[len(parts)-1]
	ownerPath := strings.Join(parts[:len(parts)-1], "/")
	return ownerPath, repoName, true
}

// IsRemote returns true if the input looks like a remote forge slug or URL.
func IsRemote(input string) bool {
	if input == "" {
		return false
	}
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return false
	}
	return DetectForge(input) != Unknown
}

// CloneURL builds a git clone URL for the given forge, slug, and optional token.
// host is only used for GitLab self-hosted (empty = default public host).
func CloneURL(kind ForgeKind, slug, host, token string) string {
	if host == "" {
		switch kind {
		case GitHub:
			host = "https://github.com"
		case GitLab:
			host = "https://gitlab.com"
		}
	}
	// Strip scheme for token injection.
	hostNoScheme := strings.TrimPrefix(host, "https://")
	hostNoScheme = strings.TrimPrefix(hostNoScheme, "http://")

	if token == "" {
		return host + "/" + slug + ".git"
	}
	switch kind {
	case GitHub:
		return "https://" + token + "@" + hostNoScheme + "/" + slug + ".git"
	case GitLab:
		return "https://oauth2:" + token + "@" + hostNoScheme + "/" + slug + ".git"
	default:
		return host + "/" + slug + ".git"
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/forge/detect.go internal/forge/detect_test.go
git commit -m "feat(forge): add URL detection, slug extraction, clone URL builder"
```

---

### Task 3: GitHub Forge Implementation

Wrap existing `internal/github/` code as a `forge.Forge` implementation. The old package stays (for backward compat during migration) but the new `forge.GitHubForge` struct implements the interface.

**Files:**
- Create: `internal/forge/github.go`
- Create: `internal/forge/github_test.go`

**Step 1: Write the tests**

Port existing tests from `internal/github/search_test.go` to use the `Forge` interface. Use same httptest pattern.

```go
// internal/forge/github_test.go
package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubForgeKind(t *testing.T) {
	f := NewGitHubForge("", "")
	if f.Kind() != GitHub {
		t.Errorf("Kind() = %v, want GitHub", f.Kind())
	}
}

func TestGitHubFetchRepoMeta(t *testing.T) {
	payload := map[string]any{
		"full_name":      "foo/bar",
		"description":    "A test repo",
		"default_branch": "main",
		"language":       "Go",
		"stargazers_count": 42,
		"forks_count":    5,
		"clone_url":      "https://github.com/foo/bar.git",
		"private":        false,
		"size":           1024,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/foo/bar" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitHubForgeWithBase("", srv.URL)
	meta, err := f.FetchRepoMeta(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.FullName != "foo/bar" {
		t.Errorf("FullName = %q, want %q", meta.FullName, "foo/bar")
	}
	if meta.Stars != 42 {
		t.Errorf("Stars = %d, want 42", meta.Stars)
	}
}

func TestGitHubSearchCode(t *testing.T) {
	payload := map[string]any{
		"items": []map[string]any{
			{
				"name":     "main.go",
				"path":     "cmd/main.go",
				"html_url": "https://github.com/foo/bar/blob/main/cmd/main.go",
				"repository": map[string]any{"full_name": "foo/bar"},
				"text_matches": []map[string]any{
					{"fragment": "func main() {"},
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitHubForgeWithBase("", srv.URL)
	results, err := f.SearchCode(context.Background(), "func main", []string{"foo/bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Repo != "foo/bar" {
		t.Errorf("Repo = %q, want %q", results[0].Repo, "foo/bar")
	}
}

func TestGitHubSearchRepos(t *testing.T) {
	payload := map[string]any{
		"items": []map[string]any{
			{
				"full_name":        "golang/go",
				"description":      "The Go programming language",
				"stargazers_count": 120000,
				"language":         "Go",
				"topics":           []string{"go"},
				"pushed_at":        "2024-06-01T12:00:00Z",
				"archived":         false,
				"html_url":         "https://github.com/golang/go",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitHubForgeWithBase("", srv.URL)
	results, err := f.SearchRepos(context.Background(), "language:go", "stars")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].FullName != "golang/go" {
		t.Errorf("unexpected results: %+v", results)
	}
}
```

**Step 2: Run tests — FAIL**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -run TestGitHub -v`

**Step 3: Implement `forge/github.go`**

Move logic from `internal/github/github.go` + `search.go` into `forge/github.go`, adapting to use shared types. Keep the code structure identical — just change the type names to `forge.*`.

The implementation should be ~180 lines (under 200 limit). Key structure:

```go
// internal/forge/github.go
package forge

// GitHubForge implements Forge for GitHub.
type GitHubForge struct {
	token   string
	apiBase string
	http    *http.Client
}

func NewGitHubForge(token, apiBase string) *GitHubForge { ... }
func newGitHubForgeWithBase(token, base string) *GitHubForge { ... } // for tests
func (g *GitHubForge) Kind() ForgeKind { return GitHub }
func (g *GitHubForge) FetchRepoMeta(...) (*RepoMeta, error) { ... }
func (g *GitHubForge) FetchREADME(...) (string, error) { ... }
func (g *GitHubForge) SearchCode(...) ([]CodeResult, error) { ... }
func (g *GitHubForge) SearchIssues(...) ([]IssueItem, error) { ... }
func (g *GitHubForge) SearchRepos(...) ([]RepoSearchResult, error) { ... }
```

**Important:** `SearchCode` and `SearchIssues` are large — split search methods into `forge/github_search.go` if `github.go` exceeds 200 lines.

**Step 4: Run tests — PASS**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -v`

**Step 5: Commit**

```bash
git add internal/forge/github.go internal/forge/github_search.go internal/forge/github_test.go
git commit -m "feat(forge): GitHub implementation of Forge interface"
```

---

### Task 4: GitLab Forge Implementation

Implement GitLab REST API v4 as a `forge.Forge` implementation.

**Files:**
- Create: `internal/forge/gitlab.go`
- Create: `internal/forge/gitlab_search.go` (if needed for line limit)
- Create: `internal/forge/gitlab_test.go`

**Step 1: Write the tests**

```go
// internal/forge/gitlab_test.go
package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitLabForgeKind(t *testing.T) {
	f := NewGitLabForge("", "")
	if f.Kind() != GitLab {
		t.Errorf("Kind() = %v, want GitLab", f.Kind())
	}
}

func TestGitLabFetchRepoMeta(t *testing.T) {
	// GitLab API: GET /projects/:id (url-encoded path)
	payload := map[string]any{
		"path_with_namespace": "group/repo",
		"description":         "A GitLab repo",
		"default_branch":      "main",
		"star_count":          10,
		"forks_count":         2,
		"http_url_to_repo":    "https://gitlab.com/group/repo.git",
		"visibility":          "public",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GitLab encodes slugs: /api/v4/projects/group%2Frepo
		if r.URL.Path != "/api/v4/projects/group%2Frepo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitLabForgeWithBase("", srv.URL)
	meta, err := f.FetchRepoMeta(context.Background(), "group/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.FullName != "group/repo" {
		t.Errorf("FullName = %q, want %q", meta.FullName, "group/repo")
	}
	if meta.Stars != 10 {
		t.Errorf("Stars = %d, want 10", meta.Stars)
	}
}

func TestGitLabFetchREADME(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET /api/v4/projects/group%2Frepo/repository/files/README.md/raw?ref=main
		if r.URL.Path == "/api/v4/projects/group%2Frepo" {
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "main"})
			return
		}
		if r.URL.Path == "/api/v4/projects/group%2Frepo/repository/files/README.md/raw" {
			w.Write([]byte("# Hello GitLab"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := newGitLabForgeWithBase("", srv.URL)
	readme, err := f.FetchREADME(context.Background(), "group/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if readme != "# Hello GitLab" {
		t.Errorf("README = %q, want %q", readme, "# Hello GitLab")
	}
}

func TestGitLabSearchRepos(t *testing.T) {
	payload := []map[string]any{
		{
			"path_with_namespace": "group/repo",
			"description":         "Found repo",
			"star_count":          5,
			"last_activity_at":    "2024-06-01T12:00:00Z",
			"archived":            false,
			"web_url":             "https://gitlab.com/group/repo",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitLabForgeWithBase("", srv.URL)
	results, err := f.SearchRepos(context.Background(), "mcp server", "stars")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].FullName != "group/repo" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestGitLabSearchCode(t *testing.T) {
	// GitLab code search requires project ID — we search within a specific project.
	payload := []map[string]any{
		{
			"filename":  "main.go",
			"path":      "cmd/main.go",
			"data":      "func main() {",
			"ref":       "main",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	f := newGitLabForgeWithBase("", srv.URL)
	results, err := f.SearchCode(context.Background(), "func main", []string{"group/repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "main.go" {
		t.Errorf("Name = %q, want %q", results[0].Name, "main.go")
	}
}

func TestGitLabAuthHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("PRIVATE-TOKEN")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "g/r",
			"default_branch": "main",
		})
	}))
	defer srv.Close()

	f := newGitLabForgeWithBase("glpat-secret", srv.URL)
	_, _ = f.FetchRepoMeta(context.Background(), "g/r")
	if gotHeader != "glpat-secret" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotHeader, "glpat-secret")
	}
}
```

**Step 2: Run tests — FAIL**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -run TestGitLab -v`

**Step 3: Implement `forge/gitlab.go`**

Key GitLab API v4 patterns:
- Project path is URL-encoded in endpoints: `/api/v4/projects/group%2Fsub%2Frepo`
- Auth: `PRIVATE-TOKEN: {token}` header
- README: First fetch project to get `default_branch`, then `GET /projects/{id}/repository/files/README.md/raw?ref={branch}`
- Code search: `GET /projects/{id}/search?scope=blobs&search={query}`
- Issues: `GET /projects/{id}/issues?search={query}`
- Repos: `GET /projects?search={query}&order_by=star_count&sort=desc`

```go
// internal/forge/gitlab.go
type GitLabForge struct {
	token   string
	apiBase string
	http    *http.Client
}

func NewGitLabForge(token, apiBase string) *GitLabForge { ... }
func (g *GitLabForge) Kind() ForgeKind { return GitLab }
func (g *GitLabForge) FetchRepoMeta(...) (*RepoMeta, error) { ... }
func (g *GitLabForge) FetchREADME(...) (string, error) { ... }
func (g *GitLabForge) SearchCode(...) ([]CodeResult, error) { ... }
func (g *GitLabForge) SearchIssues(...) ([]IssueItem, error) { ... }
func (g *GitLabForge) SearchRepos(...) ([]RepoSearchResult, error) { ... }
```

**Step 4: Run tests — PASS**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -v`

**Step 5: Commit**

```bash
git add internal/forge/gitlab*.go
git commit -m "feat(forge): GitLab implementation of Forge interface"
```

---

### Task 5: Multi-Forge Registry

Create a registry that holds configured forges and dispatches based on URL detection.

**Files:**
- Create: `internal/forge/registry.go`
- Create: `internal/forge/registry_test.go`

**Step 1: Write the tests**

```go
// internal/forge/registry_test.go
package forge

import "testing"

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge("", ""))
	r.Register(GitLab, NewGitLabForge("", ""))

	if f := r.Get(GitHub); f == nil || f.Kind() != GitHub {
		t.Error("expected GitHub forge")
	}
	if f := r.Get(GitLab); f == nil || f.Kind() != GitLab {
		t.Error("expected GitLab forge")
	}
	if f := r.Get(Unknown); f != nil {
		t.Error("expected nil for Unknown")
	}
}

func TestRegistryForURL(t *testing.T) {
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge("", ""))
	r.Register(GitLab, NewGitLabForge("", ""))

	tests := []struct {
		input string
		kind  ForgeKind
	}{
		{"https://github.com/foo/bar", GitHub},
		{"https://gitlab.com/group/repo", GitLab},
		{"foo/bar", GitHub},
		{"/local/path", Unknown},
	}
	for _, tc := range tests {
		f := r.ForURL(tc.input)
		if tc.kind == Unknown {
			if f != nil {
				t.Errorf("ForURL(%q): expected nil, got %v", tc.input, f.Kind())
			}
		} else if f == nil || f.Kind() != tc.kind {
			t.Errorf("ForURL(%q): got %v, want %v", tc.input, f, tc.kind)
		}
	}
}

func TestRegistryDefault(t *testing.T) {
	// Registry with only GitHub — bare slugs should resolve to GitHub.
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge("", ""))

	f := r.ForURL("foo/bar")
	if f == nil || f.Kind() != GitHub {
		t.Error("bare slug should resolve to GitHub when registered")
	}

	// GitLab URL should return nil if GitLab not registered.
	f = r.ForURL("https://gitlab.com/foo/bar")
	if f != nil {
		t.Error("GitLab URL should return nil when GitLab not registered")
	}
}
```

**Step 2: Run tests — FAIL**

**Step 3: Implement registry.go**

```go
// internal/forge/registry.go
package forge

// Registry holds configured forge implementations.
type Registry struct {
	forges map[ForgeKind]Forge
}

func NewRegistry() *Registry {
	return &Registry{forges: make(map[ForgeKind]Forge)}
}

func (r *Registry) Register(kind ForgeKind, f Forge) {
	r.forges[kind] = f
}

func (r *Registry) Get(kind ForgeKind) Forge {
	return r.forges[kind]
}

// ForURL detects the forge from the input and returns the matching implementation.
// Returns nil if no matching forge is registered.
func (r *Registry) ForURL(input string) Forge {
	kind := DetectForge(input)
	if kind == Unknown {
		return nil
	}
	return r.forges[kind]
}
```

**Step 4: Run tests — PASS**

Run: `cd /home/krolik/src/go-code && go test ./internal/forge/ -v`

**Step 5: Commit**

```bash
git add internal/forge/registry*.go
git commit -m "feat(forge): add Registry for multi-forge dispatch"
```

---

### Task 6: Wire Forge Into analyze.Deps and Config

Replace `*github.Client` with `*forge.Registry` in `analyze.Deps`, update config to accept GitLab env vars, update `register.go` to build the registry.

**Files:**
- Modify: `internal/analyze/analyze.go:44-67` — change `GitHub *github.Client` to `Forges *forge.Registry`
- Modify: `cmd/go-code/config.go` — add `GitLabToken`, `GitLabURL` fields
- Modify: `cmd/go-code/register.go:54` — build `forge.Registry` instead of `github.NewClient`

**Step 1: Update `analyze.Deps`**

In `internal/analyze/analyze.go`, change:
```go
// Old:
import "github.com/anatolykoptev/go-code/internal/github"
GitHub *github.Client

// New:
import "github.com/anatolykoptev/go-code/internal/forge"
Forges *forge.Registry
```

**Step 2: Update `config.go`**

Add to Config struct:
```go
// GitLabToken is the optional GitLab API token (PRIVATE-TOKEN).
GitLabToken string

// GitLabURL is the GitLab API base URL (default: https://gitlab.com).
// Set for self-hosted GitLab instances.
GitLabURL string
```

Add to `loadConfig()`:
```go
GitLabToken: env.Str("GITLAB_TOKEN", ""),
GitLabURL:   env.Str("GITLAB_URL", ""),
```

**Step 3: Update `register.go`**

Replace:
```go
GitHub: github.NewClient(cfg.GithubToken),
```
With:
```go
Forges: buildForgeRegistry(cfg),
```

Add helper:
```go
func buildForgeRegistry(cfg Config) *forge.Registry {
	reg := forge.NewRegistry()
	reg.Register(forge.GitHub, forge.NewGitHubForge(cfg.GithubToken, ""))
	if cfg.GitLabToken != "" || cfg.GitLabURL != "" {
		reg.Register(forge.GitLab, forge.NewGitLabForge(cfg.GitLabToken, cfg.GitLabURL))
	}
	return reg
}
```

**Step 4: Build to verify compilation**

Run: `cd /home/krolik/src/go-code && go build ./...`
Expected: FAIL — tool handlers still reference `deps.GitHub`

This is expected. Do NOT fix tool handlers yet — that's Task 7. Just ensure the core packages compile:

Run: `cd /home/krolik/src/go-code && go build ./internal/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/analyze/analyze.go cmd/go-code/config.go cmd/go-code/register.go
git commit -m "refactor(forge): wire Registry into Deps and config"
```

---

### Task 7: Migrate Tool Handlers to `forge.Forge`

Update all tool handlers to use `deps.Forges` instead of `deps.GitHub`. This is a mechanical find-and-replace with some logic changes for forge detection.

**Files:**
- Modify: `cmd/go-code/tool_repo_analyze.go`
- Modify: `cmd/go-code/tool_repo_search.go`
- Modify: `cmd/go-code/resolve.go`

**Step 1: Update `tool_repo_analyze.go`**

Replace all `deps.GitHub.XXX(ctx, ...)` calls with forge-from-registry pattern. The key change: when the tool receives a URL or slug, detect the forge and use the right client.

Changes needed:
1. Remove `import "github.com/anatolykoptev/go-code/internal/github"` — replace with `"github.com/anatolykoptev/go-code/internal/forge"`
2. Replace `github.ExtractOwnerRepo(...)` → `forge.ExtractOwnerRepo(...)`
3. Replace `deps.GitHub.SearchCode(...)` → get forge from registry first:
   ```go
   f := deps.Forges.ForURL(input.Repo)
   if f == nil {
       f = deps.Forges.Get(forge.GitHub) // fallback
   }
   if f == nil {
       return errResult("no forge configured"), nil
   }
   results, err := f.SearchCode(ctx, ...)
   ```
4. Same pattern for `SearchIssues`, `FetchRepoMeta`, `FetchREADME`

**Step 2: Update `tool_repo_search.go`**

1. Replace `import "github.com/anatolykoptev/go-code/internal/github"` → `"github.com/anatolykoptev/go-code/internal/forge"`
2. Replace `github.ExtractOwnerRepo(...)` → `forge.ExtractOwnerRepo(...)`
3. Replace `githubAPIRepoHits(ctx, query, sort, client *github.Client)` → use registry:
   ```go
   func forgeAPIRepoHits(ctx context.Context, query, sort string, reg *forge.Registry) []repoHit {
       var hits []repoHit
       for _, kind := range []forge.ForgeKind{forge.GitHub, forge.GitLab} {
           f := reg.Get(kind)
           if f == nil { continue }
           results, err := f.SearchRepos(ctx, query, sort)
           // ... append to hits
       }
       return hits
   }
   ```
4. Replace `deps.GitHub.FetchRepoMeta/FetchREADME` → use `deps.Forges.ForURL(hit.URL)` or `deps.Forges.Get(forge.GitHub)` fallback

**Step 3: Update `resolve.go`**

Replace the `ingest.IsRemote/NormalizeSlug/CloneRepo` chain to use `forge.IsRemote/ExtractSlug/CloneURL`:

```go
if forge.IsRemote(repo) {
    slug, ok := forge.ExtractSlug(repo)
    if !ok {
        return "", nil, fmt.Errorf("invalid repo: %q", repo)
    }
    kind := forge.DetectForge(repo)
    token := deps.GithubToken
    host := ""
    if kind == forge.GitLab {
        // Get token from config via deps — add GitLabToken to Deps or pass via closure
    }
    cloneURL := forge.CloneURL(kind, slug, host, token)
    // Use cloneURL with git clone...
}
```

**Important**: `ingest.CloneRepo` still uses git clone internally. Update it to accept a clone URL directly instead of building one. Or better: add a `forge.CloneURL` call in `resolve.go` and pass the URL to a modified `ingest.CloneRepo` that accepts a URL parameter.

Update `ingest.CloneOpts`:
```go
type CloneOpts struct {
    CloneURL string // pre-built clone URL (from forge.CloneURL)
    Ref      string
    DestDir  string
    DirName  string // slug with / replaced by _
}
```

**Step 4: Build and run full test suite**

Run: `cd /home/krolik/src/go-code && go build ./... && go test ./... -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/go-code/tool_repo_analyze.go cmd/go-code/tool_repo_search.go cmd/go-code/resolve.go internal/ingest/clone.go
git commit -m "refactor(forge): migrate tool handlers from github.Client to forge.Registry"
```

---

### Task 8: Delete Old `internal/github/` Package

Now that everything uses `internal/forge/`, remove the old package.

**Files:**
- Delete: `internal/github/github.go`
- Delete: `internal/github/search.go`
- Delete: `internal/github/search_test.go`

**Step 1: Verify no imports remain**

Run: `cd /home/krolik/src/go-code && grep -r '"github.com/anatolykoptev/go-code/internal/github"' --include='*.go'`
Expected: no output

**Step 2: Delete the package**

```bash
rm -rf internal/github/
```

**Step 3: Build and test**

Run: `cd /home/krolik/src/go-code && go build ./... && go test ./... -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor(forge): remove deprecated internal/github package"
```

---

### Task 9: Update CLAUDE.md and Documentation

Update the project's CLAUDE.md to reflect the new forge architecture.

**Files:**
- Modify: `/home/krolik/src/go-code/CLAUDE.md`

**Step 1: Update package table**

Add `internal/forge/` to the Package Overview table:
```
| `internal/forge/` | Multi-forge abstraction (GitHub, GitLab); `Forge` interface + implementations |
```

Remove `internal/github/` reference if present.

**Step 2: Update Environment Variables**

Add:
```
| `GITLAB_TOKEN` | optional | GitLab API token (PRIVATE-TOKEN) |
| `GITLAB_URL` | optional | GitLab base URL (default: `https://gitlab.com`; for self-hosted) |
```

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with forge abstraction"
```
