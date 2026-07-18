package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/forge"
)

// TestHandlePushReview_GitLabSlug_FailsClosedBeforeResolvingRoot proves
// handlePushReview (the push-review write path, mirrors reviewPRPost) also
// routes through resolvePostForge instead of hardcoding GitHub, and does so
// BEFORE the expensive resolveRoot (clone/checkout) step — so a GitLab slug
// never triggers a checkout attempt, let alone a GitHub API call.
//
// Bounded by a short deadline: if a future regression moved forge resolution
// back after resolveRoot, resolveRoot would attempt a real network clone of
// a nonexistent repo, which must not hang the test suite.
func TestHandlePushReview_GitLabSlug_FailsClosedBeforeResolvingRoot(t *testing.T) {
	gh := &fakeForge{kind: forge.GitHub}
	gl := &fakeForge{kind: forge.GitLab}
	deps := depsWithForges(gh, gl)

	errCh := make(chan error, 1)
	go func() {
		errCh <- handlePushReview("gitlab.com/acme/widgets", "abc123", "def456", deps)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error for a GitLab slug, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "GitLab") || !strings.Contains(msg, "not yet implemented") {
			t.Errorf("error %q is not the clear GitLab-not-implemented error", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handlePushReview did not fail fast on a GitLab slug — forge resolution is not happening before resolveRoot")
	}

	if gh.postCommitCalls != 0 {
		t.Errorf("GitHub forge was posted to for a GitLab slug (the bug this fixes): %d calls", gh.postCommitCalls)
	}
	if gl.postCommitCalls != 0 {
		t.Errorf("GitLab forge stub was reached instead of failing closed at the call site: %d calls", gl.postCommitCalls)
	}
}
