package repofind

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}

func TestDiscover_FindsGitSubdirs(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "repo-a"))
	gitInit(t, filepath.Join(parent, "repo-b"))
	// Non-git subdir must be skipped.
	if err := os.MkdirAll(filepath.Join(parent, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := Discover([]string{parent})
	if len(got) != 2 {
		t.Fatalf("want 2 repos, got %d: %v", len(got), got)
	}
	names := map[string]bool{}
	for _, r := range got {
		names[filepath.Base(r)] = true
	}
	if !names["repo-a"] || !names["repo-b"] {
		t.Fatalf("missing repo: %v", got)
	}
	if names["not-a-repo"] {
		t.Fatalf("non-git dir included: %v", got)
	}
}

func TestDiscover_MissingDirSkipped(t *testing.T) {
	t.Parallel()
	got := Discover([]string{"/nonexistent/parent"})
	if len(got) != 0 {
		t.Fatalf("missing dir → empty, got %v", got)
	}
}
