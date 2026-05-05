package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsGitRepo_NormalRepo(t *testing.T) {
	dir := t.TempDir()
	mustGit(t, dir, "init")
	if !IsGitRepo(dir) {
		t.Fatalf("IsGitRepo(%q) = false for a freshly initialised repo; want true", dir)
	}
}

func TestIsGitRepo_Worktree(t *testing.T) {
	// Create the main repo with at least one commit so "git worktree add" works.
	main := t.TempDir()
	mustGit(t, main, "init")
	mustGit(t, main, "commit", "--allow-empty", "-m", "init")

	wt := filepath.Join(t.TempDir(), "worktree")
	mustGit(t, main, "worktree", "add", "--detach", wt)
	t.Cleanup(func() { _ = exec.Command("git", "-C", main, "worktree", "remove", "--force", wt).Run() })

	if !IsGitRepo(wt) {
		t.Fatalf("IsGitRepo(%q) = false for a linked worktree; want true", wt)
	}
	// Verify the fixture is actually a worktree (file, not dir).
	info, err := os.Stat(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatalf("stat .git in worktree: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected .git to be a file (worktree pointer) but got a directory")
	}
}

func TestIsGitRepo_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Fatalf("IsGitRepo(%q) = true for an empty dir; want false", dir)
	}
}

func TestIsGitRepo_BareDir(t *testing.T) {
	// .git exists as a regular file but does not start with "gitdir: ".
	// We treat this as a repo (true) — downstream git commands will error
	// with a precise message if the pointer is invalid.
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("not a gitdir pointer\n"), 0o644); err != nil {
		t.Fatalf("create fake .git file: %v", err)
	}
	if !IsGitRepo(dir) {
		t.Fatalf("IsGitRepo(%q) = false when .git is a regular file; want true", dir)
	}
}

// mustGit runs a git command rooted at dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
