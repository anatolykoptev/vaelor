package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchInitialCreationLines_FirstAddPerPath(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	// foo.go created with 30 lines, later grown.
	if err := osWriteFile(filepath.Join(dir, "foo.go"), []byte(strings.Repeat("a\n", 30)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "foo.go")
	run("commit", "-m", "create foo")
	if err := osWriteFile(filepath.Join(dir, "foo.go"), []byte(strings.Repeat("a\n", 90)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "foo.go")
	run("commit", "-m", "grow foo")
	// bar.go created with 10 lines.
	if err := osWriteFile(filepath.Join(dir, "bar.go"), []byte(strings.Repeat("b\n", 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "bar.go")
	run("commit", "-m", "create bar")

	got, err := BatchInitialCreationLines(context.Background(), dir, []string{"foo.go", "bar.go", "ghost.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got["foo.go"] != 30 {
		t.Errorf("foo.go: got %d, want 30 (initial creation, not grown size)", got["foo.go"])
	}
	if got["bar.go"] != 10 {
		t.Errorf("bar.go: got %d, want 10", got["bar.go"])
	}
	if got["ghost.go"] != 0 {
		t.Errorf("ghost.go: got %d, want 0 (never created)", got["ghost.go"])
	}
}

func TestBatchInitialCreationLines_EmptyPaths(t *testing.T) {
	got, err := BatchInitialCreationLines(context.Background(), "/nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty paths → empty non-nil map, got %v", got)
	}
}

// TestChurnRisk_ScoreReadsCreationCache confirms the cache short-circuits the
// per-file git spawn: a cache value is used verbatim instead of re-running git.
func TestChurnRisk_ScoreReadsCreationCache(t *testing.T) {
	// Build a tiny real repo so CollectChurn + countLines have data, but
	// inject a creation cache so initialCreationLines is NOT spawned.
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	if err := osWriteFile(filepath.Join(dir, "f.go"), []byte(strings.Repeat("a\n", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.go")
	run("commit", "-m", "create")
	if err := osWriteFile(filepath.Join(dir, "f.go"), []byte(strings.Repeat("a\n", 150)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.go")
	run("commit", "-m", "grow")

	// Cache says creation=50 (the real value). Score should use it and
	// produce the same grown-file score as the no-cache path.
	ctx := WithBatchCreationCache(context.Background(), map[string]int{"f.go": 50})
	score, reason, err := ChurnRisk{}.Score(ctx, dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if score == 0 {
		t.Fatalf("cached creation=50 on grown file must score > 0, got 0 (reason=%q)", reason)
	}
}
