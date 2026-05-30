package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/biomarkers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mkHealthRepo creates a temporary git repo with commit messages all touching
// "foo.go". Mirrors the mkRepoWithCommits helper from internal/biomarkers
// (internal test package, not importable from here).
func mkHealthRepo(t *testing.T, msgs []string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for i, m := range msgs {
		p := filepath.Join(dir, "foo.go")
		content := []byte("package p // " + string(rune('a'+i)) + "\n")
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "foo.go")
		run("commit", "-m", m)
	}
	return dir
}

// testAgg builds the default aggregator used by the tool for test helpers.
func testAgg() *biomarkers.Aggregator {
	reg := defaultHealthRegistry()
	return biomarkers.NewAggregator(reg, defaultHealthWeights)
}

// extractFileHealthResult parses a *mcp.CallToolResult JSON body into FileHealthResult.
func extractFileHealthResult(t *testing.T, result *mcp.CallToolResult) FileHealthResult {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected non-error result, got error: %+v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", result.Content[0])
	}
	var out FileHealthResult
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("json unmarshal: %v\nbody: %s", err, tc.Text)
	}
	return out
}

// textOf extracts the raw text body from a non-error CallToolResult.
func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// TestGetFileHealth_RequiresRepo verifies that an empty Repo field returns IsError=true.
func TestGetFileHealth_RequiresRepo(t *testing.T) {
	args := FileHealthArgs{Repo: ""}
	result, err := handleFileHealthCore(context.Background(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty repo, got false")
	}
}

// TestGetFileHealth_EmptyRepo_ReturnsEmptyList tests that a non-git directory
// (CollectChurn returns error → propagated → collect churn error) with empty
// Paths yields IsError. For an actual non-git dir with explicit paths, Files is empty.
func TestGetFileHealth_EmptyRepo_ReturnsEmptyList(t *testing.T) {
	dir := t.TempDir() // not a git repo
	args := FileHealthArgs{Repo: dir, Paths: []string{}}
	result, err := handleFileHealthCore(context.Background(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// topHotspotPaths now propagates CollectChurn errors (MAJOR-3 fix).
	// A non-git tempdir causes git to exit non-zero → error returned → IsError=true.
	// That is the correct behaviour: caller should know the dir is not a git repo.
	if result.IsError {
		// Accept: non-git dir → collect churn error → IsError.
		return
	}
	out := extractFileHealthResult(t, result)
	if len(out.Files) != 0 {
		t.Fatalf("expected 0 files for non-git dir with explicit empty paths, got %d", len(out.Files))
	}
}

// TestGetFileHealth_ExplicitPaths tests that explicit paths against a real git repo
// with fix commits yields a scored FileHealth entry with Score >= 3.
func TestGetFileHealth_ExplicitPaths(t *testing.T) {
	// 3 fix commits on foo.go → prior_defect fires.
	dir := mkHealthRepo(t, []string{"fix: a", "fix: b", "fix: c"})
	args := FileHealthArgs{
		Repo:  dir,
		Paths: []string{"foo.go"},
	}
	result, err := handleFileHealthCore(context.Background(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := extractFileHealthResult(t, result)
	if len(out.Files) == 0 {
		t.Fatal("expected at least 1 file in result")
	}
	if out.Files[0].Path != "foo.go" {
		t.Fatalf("expected path foo.go, got %q", out.Files[0].Path)
	}
	// 3 fix commits → prior_defect ≈ 0.48, weight 0.6 → weighted ≈ 0.29 → score ≈ 4.
	if out.Files[0].Score < 3 {
		t.Fatalf("expected score >= 3 for 3 fix commits, got %d", out.Files[0].Score)
	}
	if out.Meta.DurationMS < 0 {
		t.Fatal("meta.duration_ms should be >= 0")
	}
}

// TestGetFileHealth_HintNeverReferencesUnregisteredTools guards against
// the broken-hint class that shipped in commit 747fdf0 (referenced
// "get_dead_code" and "understand(path=)" which don't exist / take wrong
// args). The Phase-1 contract: hints may only reference our own response
// keys (prior_defect, churn_risk) or advisory descriptions, never external
// tool names or non-existent arg keys.
func TestGetFileHealth_HintNeverReferencesUnregisteredTools(t *testing.T) {
	// High-score scenario: many fix commits → prior_defect fires hard.
	dir := mkHealthRepo(t, []string{
		"fix: a", "fix: b", "fix: c", "fix: d", "fix: e", "fix: f",
		"fix: g", "fix: h",
	})
	args := FileHealthArgs{
		Repo:  dir,
		Paths: []string{"foo.go"},
	}
	result, err := handleFileHealthCore(context.Background(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", textOf(t, result))
	}
	body := textOf(t, result)
	// The forbidden strings from the original broken hint (commit 747fdf0).
	forbidden := []string{"get_dead_code", "understand(path=", "dead_code(path="}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Fatalf("hint must not reference %q — does not exist as a tool / arg key", f)
		}
	}
}
