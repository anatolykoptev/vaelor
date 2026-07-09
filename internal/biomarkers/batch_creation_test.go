package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchInitialCreationLines_FirstAddPerPath(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	got, err := BatchInitialCreationLines(context.Background(), "/nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty paths → empty non-nil map, got %v", got)
	}
}

// TestChurnRisk_ScoreReadsCreationCache PROVES the cache short-circuits the
// per-file git spawn by injecting a SENTINEL value the real git could never
// return. A weak version (injecting the true value) would pass whether or not
// the cache is read, because the per-file fallback yields the same number —
// it cannot distinguish "cache used" from "git silently spawned". The sentinel
// makes the two paths produce DIFFERENT scores, so a green test = cache won.
func TestChurnRisk_ScoreReadsCreationCache(t *testing.T) {
	t.Parallel()
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

	// Baseline: NO cache → per-file git returns the real creation (50) →
	// rawChurn = (A+D) - 50 > 0 → score > 0.
	baseScore, _, err := ChurnRisk{}.Score(context.Background(), dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if baseScore == 0 {
		t.Fatalf("no-cache grown file must score > 0, got 0")
	}

	// Sentinel: inject creation=9999, a value git could never return for this
	// file. If the cache is read, rawChurn = (A+D) - 9999 < 0 → score 0,
	// DIFFERENT from baseScore. If the cache were ignored (git spawned), the
	// score would equal baseScore. Asserting score==0 ≠ baseScore proves the
	// cache short-circuited git.
	const sentinel = 9999
	ctx := WithBatchCreationCache(context.Background(), map[string]int{"f.go": sentinel})
	score, reason, err := ChurnRisk{}.Score(ctx, dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("sentinel creation=%d must drive score to 0 (cache read), got %v (reason=%q) — cache NOT consulted", sentinel, score, reason)
	}
	if score == baseScore {
		t.Fatalf("sentinel score must differ from no-cache baseline %v — cache was ignored", baseScore)
	}
}
