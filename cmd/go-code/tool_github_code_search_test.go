package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
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
