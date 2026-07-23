package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
)

type fakeGitHubCodeSearchForge struct{}

func (f *fakeGitHubCodeSearchForge) Kind() forge.ForgeKind { return forge.GitHub }

func (f *fakeGitHubCodeSearchForge) FetchRepoMeta(ctx context.Context, slug string) (*forge.RepoMeta, error) {
	return nil, nil
}

func (f *fakeGitHubCodeSearchForge) FetchREADME(ctx context.Context, slug string) (string, error) {
	return "", nil
}

func (f *fakeGitHubCodeSearchForge) SearchCode(ctx context.Context, query string, repos []string, opts ...forge.SearchCodeOptions) (forge.CodeSearchResult, error) {
	return forge.CodeSearchResult{
		Query: query,
		Total: 1,
		Results: []forge.CodeResult{
			{
				Name:    "main.go",
				Path:    "cmd/go-code/main.go",
				URL:     "https://github.com/owner/repo/blob/main/cmd/go-code/main.go",
				Repo:    "owner/repo",
				Content: "package main",
			},
		},
	}, nil
}

func (f *fakeGitHubCodeSearchForge) SearchIssues(ctx context.Context, query string) ([]forge.IssueItem, error) {
	return nil, nil
}

func (f *fakeGitHubCodeSearchForge) SearchRepos(ctx context.Context, query, sort string) ([]forge.RepoSearchResult, error) {
	return nil, nil
}

func TestHandleGithubCodeSearch(t *testing.T) {
	reg := forge.NewRegistry()
	reg.Register(forge.GitHub, &fakeGitHubCodeSearchForge{})
	deps := analyze.Deps{Forges: reg}

	res, err := handleGithubCodeSearch(context.Background(), GithubCodeSearchInput{Query: "func main"}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(res))
	}
	text := resultText(res)
	if !strings.Contains(text, "cmd/go-code/main.go") {
		t.Errorf("expected result to contain path; got: %s", text)
	}
	if !strings.Contains(text, "package main") {
		t.Errorf("expected result to contain fragment; got: %s", text)
	}
}

func TestHandleGithubCodeSearch_EmptyQuery(t *testing.T) {
	deps := analyze.Deps{Forges: forge.NewRegistry()}
	res, err := handleGithubCodeSearch(context.Background(), GithubCodeSearchInput{}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for empty query")
	}
	if text := resultText(res); !strings.Contains(text, "query is required") {
		t.Errorf("expected 'query is required' in error, got: %s", text)
	}
}

func TestHandleGithubCodeSearch_InvalidRepo(t *testing.T) {
	reg := forge.NewRegistry()
	reg.Register(forge.GitHub, &fakeGitHubCodeSearchForge{})
	deps := analyze.Deps{Forges: reg}

	res, err := handleGithubCodeSearch(context.Background(), GithubCodeSearchInput{Query: "func main", Repo: "not-a-repo"}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for invalid repo")
	}
	if text := resultText(res); !strings.Contains(text, "invalid repo") {
		t.Errorf("expected 'invalid repo' in error, got: %s", text)
	}
}

// errorForgeSearchCode is a fake forge whose SearchCode returns a fixed error,
// used to exercise the tool handler's transient-error hint path (#567).
type errorForgeSearchCode struct {
	fakeGitHubCodeSearchForge
	err error
}

func (f *errorForgeSearchCode) SearchCode(ctx context.Context, query string, repos []string, opts ...forge.SearchCodeOptions) (forge.CodeSearchResult, error) {
	return forge.CodeSearchResult{}, f.err
}

// TestHandleGithubCodeSearch_TransientHint guards #567: when SearchCode fails
// with a transient GitHub API error (408/5xx), the error result must include a
// hint to simplify the query (drop OR operators). Non-transient errors (4xx
// other than 408) must NOT get the hint. Reverting the IsTransientAPIError
// branch in handleGithubCodeSearch REDS the transient rows.
func TestHandleGithubCodeSearch_TransientHint(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantHint   bool
		wantSubstr string
	}{
		{
			name:       "408 timeout gets simplify hint",
			err:        forge.NewGitHubAPIError(http.StatusRequestTimeout, "github request: HTTP 408 — This query timed out"),
			wantHint:   true,
			wantSubstr: "simplify the query",
		},
		{
			name:       "503 gets simplify hint",
			err:        forge.NewGitHubAPIError(http.StatusServiceUnavailable, "github request: HTTP 503"),
			wantHint:   true,
			wantSubstr: "simplify the query",
		},
		{
			name:     "422 no hint",
			err:      forge.NewGitHubAPIError(http.StatusUnprocessableEntity, "github request: HTTP 422 — Validation Failed"),
			wantHint: false,
		},
		{
			name:     "404 no hint",
			err:      forge.NewGitHubAPIError(http.StatusNotFound, "github request: HTTP 404"),
			wantHint: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := forge.NewRegistry()
			reg.Register(forge.GitHub, &errorForgeSearchCode{err: tc.err})
			deps := analyze.Deps{Forges: reg}

			res, err := handleGithubCodeSearch(context.Background(), GithubCodeSearchInput{Query: "turn relay rotate OR failover"}, deps)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil || !res.IsError {
				t.Fatalf("expected error result")
			}
			text := resultText(res)
			hasHint := strings.Contains(text, "simplify the query")
			if hasHint != tc.wantHint {
				t.Errorf("hint presence = %v, want %v; text: %s", hasHint, tc.wantHint, text)
			}
			if tc.wantHint && !strings.Contains(text, tc.wantSubstr) {
				t.Errorf("expected %q in error, got: %s", tc.wantSubstr, text)
			}
			if tc.wantHint && !strings.Contains(text, "OR") {
				t.Errorf("hint should mention OR operators, got: %s", text)
			}
		})
	}
}
