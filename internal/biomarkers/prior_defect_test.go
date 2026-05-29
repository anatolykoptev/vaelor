package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func mkRepoWithCommits(t *testing.T, msgs []string) string {
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
	for i, m := range msgs {
		filePath := filepath.Join(dir, "foo.go")
		if err := writeFile(filePath, []byte(itoa(i)+"\n")); err != nil {
			t.Fatal(err)
		}
		run("add", "foo.go")
		run("commit", "-m", m)
	}
	return dir
}

// minimal helpers to avoid extra imports in the test
func writeFile(p string, b []byte) error {
	return osWriteFile(p, b, 0o644)
}

func TestPriorDefect_NoFixes_ScoreZero(t *testing.T) {
	dir := mkRepoWithCommits(t, []string{"feat: a", "feat: b"})
	score, reason, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("no defect-commits → score 0, got %v (%s)", score, reason)
	}
}

func TestPriorDefect_FivefixesRanksMidHigh(t *testing.T) {
	dir := mkRepoWithCommits(t, []string{
		"fix: a", "fix: b", "hotfix: c", "bug: d", "regress: e",
	})
	score, reason, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score < 0.5 || score > 0.85 {
		t.Fatalf("5 fixes → mid-high score, got %v (%s)", score, reason)
	}
	if reason == "" {
		t.Fatal("reason must not be empty when score > 0")
	}
}

func TestPriorDefect_IgnoresFeatureCommits(t *testing.T) {
	dir := mkRepoWithCommits(t, []string{
		"feat: a", "feat: b", "refactor: c", "docs: d",
	})
	score, _, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("non-defect-commits → 0, got %v", score)
	}
}
