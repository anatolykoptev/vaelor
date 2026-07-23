package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// deltaTestGitRepo builds a temp git repo with a main branch and a feature
// branch that adds a feature-only file, then returns the checkout to main.
// Returns the repo dir (sitting on main).
func deltaTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature_only.go"),
		[]byte("package main\nfunc FeatureOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature-only")
	run("checkout", "main")
	return dir
}

// TestTryHeadWorktree_CreatesAndCleans verifies that for a head ref pointing
// at a different tree than the checkout, tryHeadWorktree creates a worktree
// carrying head's tree and that the caller's Cleanup removes it (no leak).
func TestTryHeadWorktree_CreatesAndCleans(t *testing.T) {
	t.Parallel()
	dir := deltaTestGitRepo(t)

	// Precondition: main checkout has no feature_only.go.
	if _, err := os.Stat(filepath.Join(dir, "feature_only.go")); !os.IsNotExist(err) {
		t.Fatalf("precondition: main checkout must not have feature_only.go")
	}

	wt, ok, note := tryHeadWorktree(context.Background(), dir, "feature")
	if !ok {
		t.Fatalf("expected worktree creation; note=%q", note)
	}
	if wt == nil {
		t.Fatal("ok=true but wt is nil")
	}
	defer wt.Cleanup()

	// The worktree must carry the feature-only file (head's tree).
	if _, err := os.Stat(filepath.Join(wt.Path, "feature_only.go")); err != nil {
		t.Fatalf("worktree missing feature_only.go: %v", err)
	}
	// The main checkout must remain undisturbed.
	if _, err := os.Stat(filepath.Join(dir, "feature_only.go")); !os.IsNotExist(err) {
		t.Fatalf("main checkout was disturbed: feature_only.go appeared")
	}

	// Cleanup discipline: explicit Cleanup must remove the worktree path.
	path := wt.Path
	wt.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove worktree %s", path)
	}
}

// TestTryHeadWorktree_HeadEqualsCheckout_NoWorktree verifies the fast path:
// when head resolves to the same SHA as the current checkout, no worktree is
// created and no fallback note is emitted (impacted symbols reflect head's
// tree via the live checkout).
func TestTryHeadWorktree_HeadEqualsCheckout_NoWorktree(t *testing.T) {
	t.Parallel()
	dir := deltaTestGitRepo(t)

	wt, ok, note := tryHeadWorktree(context.Background(), dir, "main")
	if ok {
		if wt != nil {
			wt.Cleanup()
		}
		t.Fatal("expected no worktree when head equals checkout, got one")
	}
	if note != "" {
		t.Fatalf("expected empty note on fast path, got %q", note)
	}
	if wt != nil {
		t.Fatal("expected nil worktree on fast path")
	}
}

// TestTryHeadWorktree_UnknownRef_FallbackWithNote verifies graceful
// degradation: an unresolvable head ref produces no worktree and an honest
// fallback note, not a panic or hard failure.
func TestTryHeadWorktree_UnknownRef_FallbackWithNote(t *testing.T) {
	t.Parallel()
	dir := deltaTestGitRepo(t)

	wt, ok, note := tryHeadWorktree(context.Background(), dir, "does-not-exist")
	if ok {
		if wt != nil {
			wt.Cleanup()
		}
		t.Fatal("expected no worktree for an unknown ref")
	}
	if wt != nil {
		t.Fatal("expected nil worktree for an unknown ref")
	}
	if note == "" {
		t.Fatal("expected a fallback note for an unknown ref, got empty")
	}
	if !strings.Contains(note, "could not resolve") {
		t.Fatalf("fallback note should mention resolution failure; got %q", note)
	}
	if !strings.Contains(note, "working tree") {
		t.Fatalf("fallback note should mention working-tree fallback; got %q", note)
	}
}
