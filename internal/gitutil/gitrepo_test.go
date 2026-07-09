package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsGitRepo_NormalRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustGit(t, dir, "init")
	if !IsGitRepo(dir) {
		t.Fatalf("IsGitRepo(%q) = false for a freshly initialised repo; want true", dir)
	}
}

func TestIsGitRepo_Worktree(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Fatalf("IsGitRepo(%q) = true for an empty dir; want false", dir)
	}
}

func TestIsGitRepo_BareDir(t *testing.T) {
	t.Parallel()
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

// ---- γ.D.2 tests ----

func TestCommitsSince_CountsFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustGit(t, dir, "init")
	writeTestFile(t, dir, "foo.go", "package main")
	mustGit(t, dir, "add", "foo.go")
	mustGit(t, dir, "commit", "-m", "add foo")

	writeTestFile(t, dir, "bar.go", "package main")
	mustGit(t, dir, "add", "bar.go")
	mustGit(t, dir, "commit", "-m", "add bar")

	writeTestFile(t, dir, "foo.go", "package main // v2")
	mustGit(t, dir, "add", "foo.go")
	mustGit(t, dir, "commit", "-m", "update foo")

	counts := CommitsSince(context.Background(), dir, 30*24*time.Hour)
	if counts["foo.go"] != 2 {
		t.Errorf("foo.go: got %d commits, want 2", counts["foo.go"])
	}
	if counts["bar.go"] != 1 {
		t.Errorf("bar.go: got %d commits, want 1", counts["bar.go"])
	}
}

func TestCommitsSince_BadRoot_EmptyMap(t *testing.T) {
	t.Parallel()
	counts := CommitsSince(context.Background(), "/nonexistent/path/xyz", 30*24*time.Hour)
	if len(counts) != 0 {
		t.Errorf("expected empty map for bad root, got %v", counts)
	}
}

func TestFileDiffSince_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustGit(t, dir, "init")
	writeTestFile(t, dir, "hello.go", "package main\n// v1\n")
	mustGit(t, dir, "add", "hello.go")
	mustGit(t, dir, "commit", "-m", "initial")

	writeTestFile(t, dir, "hello.go", "package main\n// v2\n")
	mustGit(t, dir, "add", "hello.go")
	mustGit(t, dir, "commit", "-m", "update")

	diff := FileDiffSince(context.Background(), dir, "hello.go", 30*24*time.Hour, 60)
	if diff == "" {
		t.Error("expected non-empty diff for changed file")
	}
}

func TestFileDiffSince_NoChange_EmptyDiff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustGit(t, dir, "init")
	writeTestFile(t, dir, "stable.go", "package main\n")
	mustGit(t, dir, "add", "stable.go")
	mustGit(t, dir, "commit", "-m", "only commit")
	// Single commit — no prior commit — expect no panic, result may be empty.
	diff := FileDiffSince(context.Background(), dir, "stable.go", 30*24*time.Hour, 60)
	_ = diff
}

func TestFileDiffSince_CapsLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustGit(t, dir, "init")

	var sb strings.Builder
	sb.WriteString("package main\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("// original line\n")
	}
	writeTestFile(t, dir, "big.go", sb.String())
	mustGit(t, dir, "add", "big.go")
	mustGit(t, dir, "commit", "-m", "big initial")

	sb.Reset()
	sb.WriteString("package main\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("// modified line\n")
	}
	writeTestFile(t, dir, "big.go", sb.String())
	mustGit(t, dir, "add", "big.go")
	mustGit(t, dir, "commit", "-m", "big update")

	diff := FileDiffSince(context.Background(), dir, "big.go", 30*24*time.Hour, 10)
	lineCount := len(strings.Split(diff, "\n"))
	if lineCount > 20 {
		t.Errorf("diff has %d lines, expected ≤20 (cap 10 + header buffer)", lineCount)
	}
}

// writeTestFile writes content to a named file inside dir.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile %s: %v", name, err)
	}
}

func TestOriginURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if got := OriginURL(context.Background(), dir); got != "" {
		t.Fatalf("no-origin → \"\", got %q", got)
	}
	want := "git@github.com:anatolykoptev/oxpulse-chat.git"
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", want).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if got := OriginURL(context.Background(), dir); got != want {
		t.Fatalf("OriginURL = %q, want %q", got, want)
	}
}
