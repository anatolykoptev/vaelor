package explore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// initGitRepo initialises a bare-minimum git repo in dir so commits work.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("setup: %v: %s", args, out)
		}
	}
}

// commitFiles writes files and creates a commit. Returns the short SHA.
func commitFiles(t *testing.T, dir, message string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	addOut, err := exec.Command("git", "-C", dir, "add", "-A").CombinedOutput()
	if err != nil {
		t.Fatalf("git add: %v: %s", err, addOut)
	}
	commitOut, err := exec.Command("git", "-C", dir, "commit", "-m", message).CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v: %s", err, commitOut)
	}
	shaOut, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v: %s", err, shaOut)
	}
	return string(shaOut[:len(shaOut)-1]) // strip newline
}

// TestCollectRecentCommits_FilesChangedPerCommit verifies that Files on each
// CommitSummary reflects only the files touched in that single commit, not a
// cumulative count across the whole branch.
func TestCollectRecentCommits_FilesChangedPerCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	initGitRepo(t, dir)

	// commit 1: 1 file
	commitFiles(t, dir, "first commit", map[string]string{
		"a.go": "package main\n",
	})

	// commit 2: 2 files
	commitFiles(t, dir, "second commit", map[string]string{
		"b.go": "package main\n",
		"c.go": "package main\n",
	})

	// commit 3: 3 files
	commitFiles(t, dir, "third commit", map[string]string{
		"d.go": "package main\n",
		"e.go": "package main\n",
		"f.go": "package main\n",
	})

	commits, err := collectRecentCommits(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("collectRecentCommits: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("len(commits) = %d, want 3", len(commits))
	}

	// git log order is newest-first.
	want := []int{3, 2, 1}
	for i, c := range commits {
		if c.Files != want[i] {
			t.Errorf("commit[%d] (%s %q): Files = %d, want %d",
				i, c.Hash, c.Message, c.Files, want[i])
		}
	}
}

// TestCollectRecentCommits_SquashNotCumulative is the regression test for
// squash merges.  We simulate a squash by building a branch with 3 feature
// commits (touching 1 + 2 + 3 = 6 files total), then squashing them onto main
// via git merge --squash.  The squash commit must report Files = 6 (all files
// in the squash), NOT the cumulative component-commit count from git log
// --shortstat against the branch range.
//
// Note: git log --shortstat on a squash-merged commit reports the correct
// per-squash-commit diff when the commit is already on the current branch
// (because there is no "range" — it is a single commit).  The historical bug
// manifests when --shortstat is used with a range (e.g. main..feature), which
// is NOT what git log does when iterating commits on the current branch.
// The real-world symptom (2384 files for PR #23 which touched ~5 files) is
// caused by the cumulative tree-diff between the squash-merge base and HEAD of
// the feature branch, not by how git log iterates — but diff-tree still
// produces the correct per-commit answer in all cases.
func TestCollectRecentCommits_SquashNotCumulative(t *testing.T) {
	t.Parallel()
	main := t.TempDir()
	initGitRepo(t, main)

	// Seed main with an initial commit.
	commitFiles(t, main, "init", map[string]string{"README.md": "# repo\n"})

	// Create a feature branch.
	branchOut, err := exec.Command("git", "-C", main, "checkout", "-b", "feature").CombinedOutput()
	if err != nil {
		t.Fatalf("checkout -b feature: %v: %s", err, branchOut)
	}

	// Three feature commits touching 1, 2, 3 files respectively.
	commitFiles(t, main, "feat: add alpha", map[string]string{
		"alpha.go": "package main\n",
	})
	commitFiles(t, main, "feat: add beta gamma", map[string]string{
		"beta.go":  "package main\n",
		"gamma.go": "package main\n",
	})
	commitFiles(t, main, "feat: add delta epsilon zeta", map[string]string{
		"delta.go":   "package main\n",
		"epsilon.go": "package main\n",
		"zeta.go":    "package main\n",
	})

	// Switch back to main and squash-merge the feature branch.
	checkoutOut, err := exec.Command("git", "-C", main, "checkout", "master").CombinedOutput()
	if err != nil {
		// Some git versions use "main" as the default branch.
		checkoutOut, err = exec.Command("git", "-C", main, "checkout", "main").CombinedOutput()
		if err != nil {
			t.Fatalf("checkout main/master: %v: %s", err, checkoutOut)
		}
	}

	squashOut, err := exec.Command("git", "-C", main, "merge", "--squash", "feature").CombinedOutput()
	if err != nil {
		t.Fatalf("merge --squash: %v: %s", err, squashOut)
	}

	// Commit the squash.
	commitOut, err := exec.Command("git", "-C", main, "commit", "-m", "squash: merge feature branch (6 files)").CombinedOutput()
	if err != nil {
		t.Fatalf("commit squash: %v: %s", err, commitOut)
	}

	commits, err := collectRecentCommits(context.Background(), main, 5)
	if err != nil {
		t.Fatalf("collectRecentCommits: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("no commits returned")
	}

	// Newest commit is the squash; it should show 6 files.
	squash := commits[0]
	const wantFiles = 6
	if squash.Files != wantFiles {
		t.Errorf("squash commit Files = %d, want %d (must not be cumulative component count)",
			squash.Files, wantFiles)
	}
}

// TestCollectRecentCommits_InitialCommit verifies that the very first commit
// in a repo (no parent) is handled without error and counts files correctly.
func TestCollectRecentCommits_InitialCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	initGitRepo(t, dir)

	nFiles := 4
	files := make(map[string]string, nFiles)
	for i := range nFiles {
		files["file"+strconv.Itoa(i)+".go"] = "package main\n"
	}
	commitFiles(t, dir, "initial", files)

	commits, err := collectRecentCommits(context.Background(), dir, 5)
	if err != nil {
		t.Fatalf("collectRecentCommits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("len(commits) = %d, want 1", len(commits))
	}
	if commits[0].Files != nFiles {
		t.Errorf("initial commit Files = %d, want %d", commits[0].Files, nFiles)
	}
}

// TestCollectRecentCommits_NotARepo verifies the non-fatal empty result when
// the directory is not a git repository.
func TestCollectRecentCommits_NotARepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commits, err := collectRecentCommits(context.Background(), dir, 5)
	if err != nil {
		t.Fatalf("expected nil error for non-repo, got: %v", err)
	}
	if commits != nil {
		t.Errorf("expected nil commits for non-repo, got %v", commits)
	}
}
