package biomarkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
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
	t.Parallel()
	dir := mkRepoWithCommits(t, []string{"feat: a", "feat: b"})
	score, reason, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("no defect-commits → score 0, got %v (%s)", score, reason)
	}
}

func TestPriorDefect_FiveFixesRanksMidHigh(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// TestPriorDefect_AffixWordsDontMatch guards against regex regressions
// that would let "affix" / "prefix" / "fixture" / "suffix" / "debug" /
// "bugzilla" trigger the defect signal. None of these are bug-fix verbs.
func TestPriorDefect_AffixWordsDontMatch(t *testing.T) {
	t.Parallel()
	dir := mkRepoWithCommits(t, []string{
		"feat: prefix the route",
		"feat: add fixture for parser",
		"feat: suffix tooling",
		"chore: affix metadata",
		"chore: bump bugzilla integration tag",
		"refactor: debug helper extraction",
	})
	score, _, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("affix-class commits must not match, got score %v", score)
	}
}

// TestPriorDefect_ConventionalCommitFixMatches confirms the Conventional
// Commits "fix(scope): msg" shape is detected (parens are non-word, so
// the \b anchor must still allow the scope-decorated form).
func TestPriorDefect_ConventionalCommitFixMatches(t *testing.T) {
	t.Parallel()
	dir := mkRepoWithCommits(t, []string{
		"fix(rpc): handle timeout",
		"fix(ui-shell): null-check selection",
	})
	score, reason, err := PriorDefect{}.Score(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if score == 0 {
		t.Fatalf("fix(scope): commits must match, got 0 (reason=%q)", reason)
	}
}

// TestPriorDefect_PerFileParity_DiffFilter confirms that perFilePriorDefectCount
// applies --diff-filter=AMR so deletion commits are excluded, matching BatchPriorDefect.
//
// Red proof (MAJOR-1): without --diff-filter=AMR, per-file counts "fix: remove foo"
// (a deletion commit) giving count=2, while batch skips it giving count=1. Same
// (repo, path) yields different scores depending on which path fired.
func TestPriorDefect_PerFileParity_DiffFilter(t *testing.T) {
	t.Parallel()
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

	// Add and fix the file.
	if err := osWriteFile(filepath.Join(dir, "foo.go"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "foo.go")
	run("commit", "-m", "feat: initial")
	if err := osWriteFile(filepath.Join(dir, "foo.go"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "foo.go")
	run("commit", "-m", "fix: update")

	// Delete the file — this commit's subject has "fix" but --diff-filter=AMR
	// must exclude it (D = deleted).
	run("rm", "foo.go")
	run("commit", "-m", "fix: remove foo")

	// Batch uses --diff-filter=AMR → only the "fix: update" commit counts → 1.
	batchCounts, err := BatchPriorDefect(context.Background(), dir, []string{"foo.go"})
	if err != nil {
		t.Fatal(err)
	}
	batchCount := batchCounts["foo.go"]

	// Per-file must match batch: count=1, not 2.
	perFileCount, err := perFilePriorDefectCount(context.Background(), dir, "foo.go")
	if err != nil {
		t.Fatal(err)
	}

	if perFileCount != batchCount {
		t.Errorf("parity failure: batch=%d per-file=%d (per-file counts deletion commits; batch does not)",
			batchCount, perFileCount)
	}
	if batchCount != 1 {
		t.Errorf("batch count: want 1 (only fix: update), got %d", batchCount)
	}
}

// TestPriorDefect_FollowsRenames guards that a file's defect history
// survives a rename via git log --follow in the per-file path.
func TestPriorDefect_FollowsRenames(t *testing.T) {
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
	// old.go: 2 defect commits.
	if err := osWriteFile(filepath.Join(dir, "old.go"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "old.go")
	run("commit", "-m", "fix: a")
	if err := osWriteFile(filepath.Join(dir, "old.go"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "old.go")
	run("commit", "-m", "fix: b")
	// Rename old.go -> new.go.
	run("mv", "old.go", "new.go")
	run("commit", "-m", "chore: rename")

	// Per-file path (no cache) must follow the rename and count 2 defects.
	score, reason, err := PriorDefect{}.Score(context.Background(), dir, "new.go")
	if err != nil {
		t.Fatal(err)
	}
	if score == 0 {
		t.Fatalf("renamed file must carry pre-rename defect history, got 0 (reason=%q)", reason)
	}
	if !strings.Contains(reason, "2 defect-commits") {
		t.Fatalf("expected 2 defect-commits across rename, got %q", reason)
	}
}

// TestPriorDefect_ScoreReadsCacheWhenAttached confirms the cache path of
// PriorDefect.Score skips the per-file git log when the path is present in
// the attached cache. Asserted by: the cache returns a synthetic value (7)
// that the score computation must match exactly.
func TestPriorDefect_ScoreReadsCacheWhenAttached(t *testing.T) {
	t.Parallel()
	cache := map[string]int{"foo.go": 7}
	ctx := WithBatchDefectCache(context.Background(), cache)
	// repoRoot is bogus on purpose — if the cache is NOT consulted, the
	// per-file git log would error out and we'd see a non-nil err.
	score, reason, err := PriorDefect{}.Score(ctx, "/nonexistent", "foo.go")
	if err != nil {
		t.Fatalf("cache hit must skip git: got err %v", err)
	}
	if score == 0 {
		t.Fatal("cached count=7 must produce non-zero score")
	}
	if !strings.Contains(reason, "7 defect-commits") {
		t.Fatalf("reason should cite 7, got %q", reason)
	}
}
