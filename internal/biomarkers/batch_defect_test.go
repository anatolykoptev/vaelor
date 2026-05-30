package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBatchPriorDefect_ReturnsCountsPerPath(t *testing.T) {
	dir := t.TempDir()
	run := func(env []string, args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-b", "main")
	run(nil, "config", "user.email", "t@t.t")
	run(nil, "config", "user.name", "t")
	seq := 0
	commitOn := func(path, msg string) {
		seq++
		if err := osWriteFile(filepath.Join(dir, path), []byte(itoa(seq)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", path)
		run(nil, "commit", "-m", msg)
	}
	// foo.go: 3 defect, 1 feature
	commitOn("foo.go", "fix: a")
	commitOn("foo.go", "fix: b")
	commitOn("foo.go", "hotfix: c")
	commitOn("foo.go", "feat: d")
	// bar.go: 1 defect
	commitOn("bar.go", "bug: e")
	// baz.go: 0 defect
	commitOn("baz.go", "feat: f")

	counts, err := BatchPriorDefect(context.Background(), dir, []string{"foo.go", "bar.go", "baz.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := counts["foo.go"]; got != 3 {
		t.Errorf("foo.go: got %d defect-commits, want 3", got)
	}
	if got := counts["bar.go"]; got != 1 {
		t.Errorf("bar.go: got %d, want 1", got)
	}
	if got := counts["baz.go"]; got != 0 {
		t.Errorf("baz.go: got %d, want 0", got)
	}
}

func TestBatchPriorDefect_EmptyPaths(t *testing.T) {
	counts, err := BatchPriorDefect(context.Background(), "/nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts == nil {
		t.Fatal("counts must be non-nil even for empty input")
	}
	if len(counts) != 0 {
		t.Fatalf("counts must be empty, got %v", counts)
	}
}
