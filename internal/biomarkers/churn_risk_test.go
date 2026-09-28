package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/compare"
)

func mkRepoWithChurn(t *testing.T, lines int, churnCycles int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = gitTestEnv()
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
		dumpChurnDiag(t, dir)
		t.Fatal(err)
	}
	if score < 0.9 {
		dumpChurnDiag(t, dir)
		t.Fatalf("heavily-rewritten file → ≥0.9, got %v (%s)", score, reason)
	}
}

// dumpChurnDiag dumps the git state and intermediate numbers behind a
// ChurnRisk.Score failure, so the next flake is self-diagnosing (#811 —
// the first observed failure's CI log was lost before capture).
func dumpChurnDiag(t *testing.T, dir string) {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "log", "--numstat",
		"--pretty=format:%H %ad", "--no-merges").CombinedOutput()
	t.Logf("git log --numstat:\n%s", out)
	stats, err := compare.CollectChurn(context.Background(), dir, 90*24*time.Hour)
	t.Logf("CollectChurn err=%v stats=%+v", err, stats)
	t.Logf("initialCreationLines=%d loc-check=%d",
		initialCreationLines(context.Background(), dir, "f.go"),
		mustCountLines(t, filepath.Join(dir, "f.go")))
}

func mustCountLines(t *testing.T, path string) int {
	t.Helper()
	n, err := countLines(path)
	if err != nil {
		t.Logf("countLines: %v", err)
	}
	return n
}

// TestChurnRisk_GrownFileScoresNonZero guards the growth blind spot: a
// file created small then grown substantially post-creation has real
// churn that the old (A+D-LOC) formula zeroed out.
func TestChurnRisk_GrownFileScoresNonZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = gitTestEnv()
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
