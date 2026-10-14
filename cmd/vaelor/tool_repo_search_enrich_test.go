package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
)

// Fixtures under testdata/repo_search are real API responses captured from
// api.github.com and gitlab.com (see the PR for capture commands). The
// *_archived GitLab fixture is the real response with "archived": true added,
// because gitlab.com omits that key for unauthenticated callers.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "repo_search", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func fixtureField[T any](t *testing.T, raw []byte, key string) T {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	var v T
	if err := json.Unmarshal(m[key], &v); err != nil {
		t.Fatalf("fixture key %q: %v", key, err)
	}
	return v
}

func TestEnrichRepoResults_PassesPushTopicsArchived(t *testing.T) {
	tests := []struct {
		name         string
		kind         forge.ForgeKind
		fixture      string
		hitURL       string
		owner, repo  string
		pushedKey    string
		wantArchived bool
	}{
		{"github", forge.GitHub, "github_repo.json", "https://github.com/cli/cli", "cli", "cli", "pushed_at", false},
		{"github archived", forge.GitHub, "github_repo_archived.json", "https://github.com/atom/atom", "atom", "atom", "pushed_at", true},
		{"gitlab", forge.GitLab, "gitlab_project.json", "https://gitlab.com/gitlab-org/gitlab", "gitlab-org", "gitlab", "last_activity_at", false},
		{"gitlab archived", forge.GitLab, "gitlab_project_archived.json", "https://gitlab.com/gitterHQ/webapp", "gitterHQ", "webapp", "last_activity_at", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := readFixture(t, tc.fixture)
			wantPush := fixtureField[string](t, body, tc.pushedKey)
			wantTopics := fixtureField[[]string](t, body, "topics")
			if wantPush == "" || len(wantTopics) == 0 {
				t.Fatalf("fixture %s must carry a push date and topics", tc.fixture)
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/readme") || strings.HasSuffix(r.URL.Path, "/repository/files/README.md/raw") {
					_, _ = w.Write([]byte("readme"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			reg := forge.NewRegistry()
			switch tc.kind {
			case forge.GitHub:
				reg.Register(forge.GitHub, forge.NewGitHubForge("", forge.AppConfig{}, forge.WithAPIBase(srv.URL)))
			case forge.GitLab:
				reg.Register(forge.GitLab, forge.NewGitLabForge("", srv.URL))
			}

			enriched := enrichRepoResults(context.Background(),
				[]repoHit{{Owner: tc.owner, Repo: tc.repo, URL: tc.hitURL}},
				analyze.Deps{Forges: reg})
			got := buildRepoSearchContext(enriched)

			if want := "Last push: " + wantPush + "\n"; !strings.Contains(got, want) {
				t.Errorf("missing %q in context:\n%s", want, got)
			}
			if want := "Topics: " + strings.Join(wantTopics, ", ") + "\n"; !strings.Contains(got, want) {
				t.Errorf("missing %q in context:\n%s", want, got)
			}
			if has := strings.Contains(got, "Status: ARCHIVED"); has != tc.wantArchived {
				t.Errorf("ARCHIVED marker = %v, want %v:\n%s", has, tc.wantArchived, got)
			}
		})
	}
}
