package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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

// TestGetFileHealth_RequiresRepo verifies that an empty Repo field returns IsError=true.
func TestGetFileHealth_RequiresRepo(t *testing.T) {
	args := FileHealthArgs{Repo: ""}
	result, err := handleFileHealthCore(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty repo, got false")
	}
}

// TestGetFileHealth_EmptyRepo_ReturnsEmptyList tests that a non-git directory
// (CollectChurn returns nil/empty) with empty Paths yields empty Files and no error.
func TestGetFileHealth_EmptyRepo_ReturnsEmptyList(t *testing.T) {
	dir := t.TempDir() // not a git repo
	args := FileHealthArgs{Repo: dir, Paths: []string{}}
	result, err := handleFileHealthCore(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("unexpected error result: %s", tc.Text)
			}
		}
		t.Fatalf("unexpected error result")
	}
	out := extractFileHealthResult(t, result)
	if len(out.Files) != 0 {
		t.Fatalf("expected 0 files for non-git dir, got %d", len(out.Files))
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
	result, err := handleFileHealthCore(context.Background(), args)
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
