package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
)

// fakeForge is a minimal forge.Forge + forge.Poster test double. It performs
// no I/O and records every write call, so tests can assert exactly which
// forge a code path posted through without hitting a real network.
type fakeForge struct {
	kind forge.ForgeKind

	postReviewCalls   int
	postReviewSlug    string
	postReviewPR      int
	postReviewPayload forge.ReviewPayload
	postReviewURL     string
	postReviewErr     error

	postCommitCalls int
	postCommitSlug  string
	postCommitSHA   string
	postCommitBody  string
	postCommitURL   string
	postCommitErr   error
}

func (f *fakeForge) Kind() forge.ForgeKind { return f.kind }

func (f *fakeForge) FetchRepoMeta(context.Context, string) (*forge.RepoMeta, error) {
	return nil, errors.New("fakeForge: FetchRepoMeta not implemented")
}

func (f *fakeForge) FetchREADME(context.Context, string) (string, error) { return "", nil }

func (f *fakeForge) SearchCode(context.Context, string, []string, ...forge.SearchCodeOptions) (forge.CodeSearchResult, error) {
	return forge.CodeSearchResult{}, nil
}

func (f *fakeForge) SearchIssues(context.Context, string) ([]forge.IssueItem, error) {
	return nil, nil
}

func (f *fakeForge) SearchRepos(context.Context, string, string) ([]forge.RepoSearchResult, error) {
	return nil, nil
}

func (f *fakeForge) PostReview(_ context.Context, slug string, pr int, p forge.ReviewPayload) (string, error) {
	f.postReviewCalls++
	f.postReviewSlug = slug
	f.postReviewPR = pr
	f.postReviewPayload = p
	return f.postReviewURL, f.postReviewErr
}

func (f *fakeForge) PostIssueComment(context.Context, string, int, string) (string, error) {
	return "", nil
}

func (f *fakeForge) PostCommitComment(_ context.Context, slug, sha, body string) (string, error) {
	f.postCommitCalls++
	f.postCommitSlug = slug
	f.postCommitSHA = sha
	f.postCommitBody = body
	return f.postCommitURL, f.postCommitErr
}

// depsWithForges builds an analyze.Deps carrying a registry with the given
// fakes registered under their own Kind().
func depsWithForges(fakes ...*fakeForge) analyze.Deps {
	reg := forge.NewRegistry()
	for _, f := range fakes {
		reg.Register(f.kind, f)
	}
	return analyze.Deps{Forges: reg}
}

func TestResolvePostForge_GitLabInput_ReturnsClearErrorWithoutTouchingAnyForge(t *testing.T) {
	gh := &fakeForge{kind: forge.GitHub}
	gl := &fakeForge{kind: forge.GitLab}
	deps := depsWithForges(gh, gl)

	inputs := []string{
		"https://gitlab.com/acme/widgets",
		"gitlab.com/acme/widgets",
		"git@gitlab.com:acme/widgets.git",
	}
	for _, repo := range inputs {
		t.Run(repo, func(t *testing.T) {
			p, err := resolvePostForge(deps, repo)
			if p != nil {
				t.Fatalf("resolvePostForge(%q) returned non-nil poster, want nil", repo)
			}
			if err == nil {
				t.Fatalf("resolvePostForge(%q) returned nil error, want a GitLab-not-implemented error", repo)
			}
			msg := err.Error()
			if !strings.Contains(msg, "GitLab") {
				t.Errorf("error %q does not mention GitLab", msg)
			}
			if !strings.Contains(msg, "not yet implemented") {
				t.Errorf("error %q is not a clear not-implemented message", msg)
			}
			if !strings.Contains(msg, repo) {
				t.Errorf("error %q does not name the repo %q", msg, repo)
			}
		})
	}
	// The whole point of failing closed on GitLab is to never touch a forge.
	if gh.postReviewCalls != 0 || gh.postCommitCalls != 0 {
		t.Errorf("GitHub forge was called for GitLab input: %+v", gh)
	}
	if gl.postReviewCalls != 0 || gl.postCommitCalls != 0 {
		t.Errorf("GitLab forge stub was called (should fail before reaching it): %+v", gl)
	}
}

func TestResolvePostForge_GitHubInput_RoutesToRegisteredGitHubForge(t *testing.T) {
	gh := &fakeForge{kind: forge.GitHub}
	gl := &fakeForge{kind: forge.GitLab}
	deps := depsWithForges(gh, gl)

	inputs := []string{
		"acme/widgets",
		"github.com/acme/widgets",
		"https://github.com/acme/widgets",
		"git@github.com:acme/widgets.git",
	}
	for _, repo := range inputs {
		t.Run(repo, func(t *testing.T) {
			p, err := resolvePostForge(deps, repo)
			if err != nil {
				t.Fatalf("resolvePostForge(%q) returned unexpected error: %v", repo, err)
			}
			got, ok := p.(*fakeForge)
			if !ok || got != gh {
				t.Fatalf("resolvePostForge(%q) did not return the registered GitHub forge; got %#v", repo, p)
			}
		})
	}
}

func TestResolvePostForge_BareSlugNoRegistryEntry_FallsBackToGitHub(t *testing.T) {
	// Mirrors the read-path fallback (handleQuickMode / handleIssuesMode):
	// a bare owner/repo slug with only GitHub registered still resolves.
	gh := &fakeForge{kind: forge.GitHub}
	deps := depsWithForges(gh)

	p, err := resolvePostForge(deps, "acme/widgets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != forge.Poster(gh) {
		t.Fatalf("expected fallback to the registered GitHub forge, got %#v", p)
	}
}

func TestResolvePostForge_NilRegistry_ReturnsError(t *testing.T) {
	deps := analyze.Deps{}
	p, err := resolvePostForge(deps, "acme/widgets")
	if p != nil || err == nil {
		t.Fatalf("expected (nil, error) for a nil registry; got (%#v, %v)", p, err)
	}
}

func TestResolvePostForge_NoGitHubRegistered_ReturnsError(t *testing.T) {
	deps := depsWithForges() // empty registry
	p, err := resolvePostForge(deps, "acme/widgets")
	if p != nil || err == nil {
		t.Fatalf("expected (nil, error) when no forge is configured; got (%#v, %v)", p, err)
	}
	if !strings.Contains(err.Error(), "no forge configured") {
		t.Errorf("error %q does not explain the missing forge", err.Error())
	}
}
