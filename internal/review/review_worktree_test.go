package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mustGit runs a git command inside dir and fatals the test on error.
// This mirrors the helper pattern from diff_test.go's setupGitRepo.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupRepoWithWorktree creates a main repo with two commits and adds a worktree
// on a feature branch. Returns (mainDir, worktreeDir).
func setupRepoWithWorktree(t *testing.T) (mainDir, worktreeDir string) {
	t.Helper()

	mainDir = t.TempDir()
	mustGit(t, mainDir, "init")
	mustGit(t, mainDir, "config", "user.email", "test@test.com")
	mustGit(t, mainDir, "config", "user.name", "test")

	// Initial commit.
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, mainDir, "add", ".")
	mustGit(t, mainDir, "commit", "-m", "initial")

	// Second commit — this is what a delta review will diff against HEAD~1.
	if err := os.WriteFile(filepath.Join(mainDir, "util.go"), []byte("package main\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, mainDir, "add", ".")
	mustGit(t, mainDir, "commit", "-m", "add helper")

	// Add a worktree on a new branch.
	worktreeDir = filepath.Join(t.TempDir(), "feature-wt")
	mustGit(t, mainDir, "worktree", "add", "-b", "feature", worktreeDir, "HEAD")

	return mainDir, worktreeDir
}

// TestReviewDelta_OnWorktree verifies that ChangedFiles succeeds when the
// repo root is a git worktree (where .git is a pointer file, not a directory).
// This is the regression guard for the "fatal: not a git repository:
// .../.git/worktrees/<name>" error that occurred when go-code's review_delta
// MCP tool was invoked with a worktree path.
func TestReviewDelta_OnWorktree(t *testing.T) {
	t.Parallel()
	_, worktreeDir := setupRepoWithWorktree(t)

	// Confirm .git in worktree is a file (not a dir) — the precondition that
	// caused the original bug.
	info, err := os.Stat(filepath.Join(worktreeDir, ".git"))
	if err != nil {
		t.Fatalf("stat worktreeDir/.git: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("expected worktreeDir/.git to be a regular file (worktree pointer), got mode %v", info.Mode())
	}

	// ChangedFiles must not fail on a worktree path.
	// No path-rewrite needed here because the gitdir path in .git points to
	// a location that exists on this machine (same filesystem, no container mapping).
	files, err := ChangedFiles(context.Background(), worktreeDir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("ChangedFiles on worktree: %v", err)
	}

	// The second commit added util.go — we should see it.
	found := false
	for _, f := range files {
		if f.Path == "util.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected util.go in diff; got: %v", files)
	}
}

// TestReviewDelta_OnWorktreeWithPathRewrite verifies that ChangedFilesRewrite
// correctly applies a pathRewrite function when the gitdir path in the worktree
// .git file uses a different prefix (simulating container PATH_MAPPINGS).
// The test creates a "fake remapped" .git file and a shadow gitdir tree to
// confirm the rewrite is applied before git is invoked.
func TestReviewDelta_OnWorktreeWithPathRewrite(t *testing.T) {
	t.Parallel()
	_, worktreeDir := setupRepoWithWorktree(t)

	// Read the actual .git file to find the real gitdir.
	data, err := os.ReadFile(filepath.Join(worktreeDir, ".git"))
	if err != nil {
		t.Fatalf("read .git file: %v", err)
	}
	realGitdir := strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir: ")

	// Create a "fake" worktree dir that has a .git file pointing to a
	// path with a different prefix (simulating /home/user → /host mapping).
	fakeBase := t.TempDir()
	fakeWorktreeDir := filepath.Join(fakeBase, "fake-wt")
	if err := os.MkdirAll(fakeWorktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The fake .git file uses a "/fake-external" prefix instead of the real path prefix.
	fakeGitdir := "/fake-external" + realGitdir // deliberately broken path
	if err := os.WriteFile(filepath.Join(fakeWorktreeDir, ".git"),
		[]byte("gitdir: "+fakeGitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// pathRewrite maps /fake-external back to "": i.e., strips the prefix.
	// This simulates the container-to-host remapping.
	rewrite := func(p string) string {
		return strings.TrimPrefix(p, "/fake-external")
	}

	// resolveGitArgs must translate fakeGitdir → realGitdir via rewrite.
	finalArgs, _ := resolveGitArgs(fakeWorktreeDir, rewrite, []string{"diff", "--numstat", "HEAD~1", "HEAD"})

	// Verify the resolved --git-dir uses the real gitdir.
	expectedGitDirArg := "--git-dir=" + realGitdir
	if len(finalArgs) == 0 || finalArgs[0] != expectedGitDirArg {
		t.Fatalf("expected first arg %q; got args %v", expectedGitDirArg, finalArgs)
	}

	// Now run the actual git command via the worktree dir (not fakeWorktreeDir)
	// to confirm ChangedFilesRewrite succeeds end-to-end on a real worktree.
	files, err := ChangedFilesRewrite(context.Background(), worktreeDir, nil, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("ChangedFilesRewrite on real worktree: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected changed files, got none")
	}
}

// TestReviewDelta_OnNormalRepo is a regression guard: normal (non-worktree)
// repos must continue to work after the worktree fix.
func TestReviewDelta_OnNormalRepo(t *testing.T) {
	t.Parallel()
	dir := setupGitRepo(t) // reuse helper from diff_test.go

	files, err := ChangedFiles(context.Background(), dir, "HEAD~1", "")
	if err != nil {
		t.Fatalf("ChangedFiles on normal repo: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected changed files from normal repo, got none")
	}
}
