package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Issue #905: a cold repository must return a "building" status quickly
// instead of blocking inside DeltaReview for the whole call-graph build.
func TestGateDeltaCallGraph_ColdRepoReturnsBuilding(t *testing.T) {
	dir := t.TempDir()
	callgraph.InvalidateBuildCache()

	res := gateDeltaCallGraph("review_delta", dir, dir, dir, "go")
	if res == nil {
		t.Fatal("cold repo must return a building status result, not proceed")
	}
	if res.IsError {
		t.Fatal("building status must be a normal result, not an error")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	if !strings.Contains(tc.Text, "<status>building</status>") {
		t.Errorf("response missing building status: %s", tc.Text)
	}
	if !strings.Contains(tc.Text, "retry") {
		t.Errorf("response must tell the caller to retry: %s", tc.Text)
	}
	// Drain the background prefetch so it cannot leak CPU into later
	// tests in this binary — and prove the retry the response promises
	// would actually converge.
	waitForWarm(t, dir, "go")
}

// A warm repository must pass through (nil result) so the call proceeds
// synchronously — the gate must not turn cached calls into a retry loop.
func TestGateDeltaCallGraph_WarmRepoProceeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tiny.go"),
		[]byte("package tiny\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callgraph.InvalidateBuildCache()

	// Complete a real build so cgCache holds the entry.
	if _, err := callgraph.BuildFromRepo(t.Context(), callgraph.TraceRepoInput{Root: dir}); err != nil {
		t.Fatalf("warm build: %v", err)
	}
	if res := gateDeltaCallGraph("review_delta", dir, dir, dir, ""); res != nil {
		t.Errorf("warm repo must proceed synchronously, got status: %v", res)
	}
}

// A head worktree (analysisRoot != root) keeps synchronous behaviour: the
// cache key is a per-call temp path that a background prefetch could never
// converge, so gating there would turn every head review into an infinite
// building loop.
func TestGateDeltaCallGraph_WorktreeProceeds(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	callgraph.InvalidateBuildCache()
	if res := gateDeltaCallGraph("review_delta", root, root, wt, ""); res != nil {
		t.Error("worktree path must not be gated — it cannot converge")
	}
}

// Issue #905 end-to-end: a cold repository must reach handleReviewDelta and
// return the building contract quickly — this exercises the real call site,
// not just the gate helper (mutating the gate to always-proceed must fail
// this test by entering the synchronous build instead).
func TestHandleReviewDelta_ColdRepoReturnsBuilding(t *testing.T) {
	dir := deltaTestGitRepo(t)
	callgraph.InvalidateBuildCache()

	res, err := handleReviewDelta(context.Background(), ReviewDeltaInput{
		Repo: dir,
		Base: "HEAD",
	}, analyze.Deps{}, nil)
	if err != nil {
		t.Fatalf("handleReviewDelta: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("building status must be a normal result, not an error: %v", res)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	if !strings.Contains(tc.Text, "<status>building</status>") {
		t.Errorf("expected building status from cold repo, got: %s", tc.Text)
	}
	// Same drain: the building answer must be backed by a real converging
	// warm, and the goroutine must not outlive the test.
	waitForWarm(t, dir, "")
}

// waitForWarm polls until cgCache holds a build for (root, language) —
// the convergence the "building" status promises — or fails the test.
// It also bounds background prefetch lifetime inside the test binary.
func waitForWarm(t *testing.T, root, language string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !callgraph.ReadyForBuild(callgraph.TraceRepoInput{Root: root, Language: language}) {
		if time.Now().After(deadline) {
			t.Fatal("background prefetch did not converge to a warm cache within 60s")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
