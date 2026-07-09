package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mkRepoWithChurn(t *testing.T, lines int, churnCycles int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for cycle := 0; cycle < churnCycles; cycle++ {
		body := strings.Repeat("a\n", lines)
		if cycle%2 == 1 {
			body = strings.Repeat("b\n", lines)
		}
		if err := osWriteFile(filepath.Join(dir, "f.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "f.go")
		run("commit", "-m", "churn cycle")
	}
	return dir
}

func TestChurnRisk_StableFileZero(t *testing.T) {
	t.Parallel()
	dir := mkRepoWithChurn(t, 50, 1) // one commit, never edited again
	score, _, err := ChurnRisk{}.Score(context.Background(), dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("stable file → 0, got %v", score)
	}
}

func TestChurnRisk_RewrittenFileHighScore(t *testing.T) {
	t.Parallel()
	dir := mkRepoWithChurn(t, 50, 6) // 6 cycles * 50 lines ≈ 300 line-changes / 50 LOC = 6
	score, reason, err := ChurnRisk{}.Score(context.Background(), dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if score < 0.9 {
		t.Fatalf("heavily-rewritten file → ≥0.9, got %v (%s)", score, reason)
	}
}

// TestChurnRisk_GrownFileScoresNonZero guards the growth blind spot: a
// file created small then grown substantially post-creation has real
// churn that the old (A+D-LOC) formula zeroed out.
func TestChurnRisk_GrownFileScoresNonZero(t *testing.T) {
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
	// Commit 1: create f.go with 50 lines.
	if err := osWriteFile(filepath.Join(dir, "f.go"), []byte(strings.Repeat("a\n", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.go")
	run("commit", "-m", "create")
	// Commit 2: grow to 150 lines (3x growth — heavy post-creation churn).
	if err := osWriteFile(filepath.Join(dir, "f.go"), []byte(strings.Repeat("a\n", 150)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.go")
	run("commit", "-m", "grow 3x")

	score, reason, err := ChurnRisk{}.Score(context.Background(), dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if score == 0 {
		t.Fatalf("grown file must score > 0, got 0 (reason=%q)", reason)
	}
}
