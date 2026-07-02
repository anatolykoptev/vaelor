package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/review"
)

// These tests exercise the REAL reviewPRPost (dry_run=false write path) end
// to end, proving the fix for the multi-forge routing bug: reviewPRPost used
// to hardcode forge.NewGitHubForge(GITHUB_TOKEN, AppConfig{}) regardless of
// input.Repo, so a GitLab-hosted PR silently posted (or attempted to post)
// against the GitHub API. It must now resolve the forge from input.Repo via
// deps.Forges (resolvePostForge, forge_post.go) and fail closed with a clear
// error for GitLab instead of ever reaching a real GitHub/GitLab client.
//
// GITHUB_TOKEN is deliberately left unset for the GitLab case: the pre-fix
// code checked GITHUB_TOKEN before doing anything else, so ANY repo — GitLab
// included — with no token set returned "GITHUB_TOKEN not set". Post-fix,
// forge resolution happens first, so a GitLab repo gets the GitLab-specific
// error even with GITHUB_TOKEN unset. Asserting the message does NOT contain
// "GITHUB_TOKEN" is what makes this test RED against the pre-fix code and
// GREEN against the fix, without making any network call in either outcome.
func TestReviewPRPost_GitLabRepo_FailsClosedBeforeAnyGitHubCall(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	gh := &fakeForge{kind: forge.GitHub}
	gl := &fakeForge{kind: forge.GitLab}
	deps := depsWithForges(gh, gl)

	input := ReviewPRInput{Repo: "gitlab.com/acme/widgets", PR: 7, Event: "COMMENT"}
	result := &review.DeltaResult{Risk: review.RiskGuidance{RiskLevel: "low", RiskScore: 0.1}}

	res, err := reviewPRPost(context.Background(), input, deps, "/tmp/root", result, nil)
	if err != nil {
		t.Fatalf("reviewPRPost returned unexpected transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result for a GitLab repo, got success: %+v", res)
	}
	msg := resultText(res)
	if !strings.Contains(msg, "GitLab") || !strings.Contains(msg, "not yet implemented") {
		t.Errorf("error message %q is not the clear GitLab-not-implemented error", msg)
	}
	if strings.Contains(msg, "GITHUB_TOKEN") {
		t.Errorf("error message %q leaked the GITHUB_TOKEN check — forge resolution did not happen first (regression to pre-fix ordering)", msg)
	}
	if gh.postReviewCalls != 0 {
		t.Errorf("GitHub forge was posted to for a GitLab repo (the bug this fixes): %d calls, slug=%q", gh.postReviewCalls, gh.postReviewSlug)
	}
	if gl.postReviewCalls != 0 {
		t.Errorf("GitLab forge stub was reached instead of failing closed at the call site: %d calls", gl.postReviewCalls)
	}
}

func TestReviewPRPost_GitHubRepo_PostsViaRegisteredForge_NoRegression(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "dummy-token-for-precondition-check-only")

	gh := &fakeForge{kind: forge.GitHub, postReviewURL: "https://github.com/acme/widgets/pull/7#pullrequestreview-1"}
	gl := &fakeForge{kind: forge.GitLab}
	deps := depsWithForges(gh, gl)

	input := ReviewPRInput{Repo: "acme/widgets", PR: 7, Event: "APPROVE"}
	result := &review.DeltaResult{Risk: review.RiskGuidance{RiskLevel: "low", RiskScore: 0.1}}

	res, err := reviewPRPost(context.Background(), input, deps, "/tmp/root", result, nil)
	if err != nil {
		t.Fatalf("reviewPRPost returned unexpected transport error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success for a GitHub repo, got error: %s", resultText(res))
	}
	if gh.postReviewCalls != 1 {
		t.Fatalf("want exactly 1 PostReview call on the registered GitHub forge, got %d", gh.postReviewCalls)
	}
	if gh.postReviewSlug != "acme/widgets" || gh.postReviewPR != 7 {
		t.Errorf("PostReview called with wrong slug/pr: slug=%q pr=%d", gh.postReviewSlug, gh.postReviewPR)
	}
	if gl.postReviewCalls != 0 {
		t.Errorf("GitLab forge unexpectedly touched for a GitHub repo: %d calls", gl.postReviewCalls)
	}
	msg := resultText(res)
	if !strings.Contains(msg, gh.postReviewURL) {
		t.Errorf("success text %q does not surface the posted URL %q", msg, gh.postReviewURL)
	}
}
