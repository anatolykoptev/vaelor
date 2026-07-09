package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCreatePRWorktree verifies the worktree carries the PR ref's tree,
// independent of the source clone's checkout.
func TestCreatePRWorktree(t *testing.T) {
	t.Parallel()
	dir := setupGitRepo(t)
	run := func(args ...string) []byte {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, _ := cmd.CombinedOutput()
		return out
	}
	// Build a side branch with a unique file, return source clone to main.
	mainRef := string(run("rev-parse", "--abbrev-ref", "HEAD"))
	mainRef = filepath.Clean(mainRef[:len(mainRef)-1]) // strip newline
	run("checkout", "-b", "pr-feature")
	if err := os.WriteFile(filepath.Join(dir, "pr_only.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "PR-only commit")
	run("checkout", mainRef)

	// Source clone HEAD does NOT contain pr_only.go.
	if _, err := os.Stat(filepath.Join(dir, "pr_only.go")); !os.IsNotExist(err) {
		t.Fatalf("pre-condition: source clone should not have pr_only.go")
	}

	// Create worktree at the PR ref.
	wt, err := CreatePRWorktree(context.Background(), dir, "pr-feature")
	if err != nil {
		t.Fatalf("CreatePRWorktree: %v", err)
	}
	defer wt.Cleanup()

	// Worktree should have pr_only.go because it was checked out at pr-feature.
	if _, err := os.Stat(filepath.Join(wt.Path, "pr_only.go")); err != nil {
		t.Fatalf("worktree missing pr_only.go: %v", err)
	}

	// Source clone tree must remain untouched (still on mainRef).
	if _, err := os.Stat(filepath.Join(dir, "pr_only.go")); !os.IsNotExist(err) {
		t.Fatalf("source clone tree was disturbed: pr_only.go appeared")
	}
}

// TestCreatePRWorktree_Cleanup verifies cleanup removes the worktree dir.
func TestCreatePRWorktree_Cleanup(t *testing.T) {
	t.Parallel()
	dir := setupGitRepo(t)
	wt, err := CreatePRWorktree(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatalf("CreatePRWorktree: %v", err)
	}
	path := wt.Path
	wt.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove %s", path)
	}
}
