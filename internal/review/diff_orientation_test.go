package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// setupOrientationRepo builds a repo where the second commit makes a
// deliberately UNEQUAL, non-zero change. An added/removed swap anywhere in
// the pipeline flips the numbers and fails the assertions below — an
// equal-count fixture cannot detect the swap (#764).
func setupOrientationRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	base := "package main\n\nfunc work() int {\n\ta := 1\n\tb := 2\n\treturn a + b\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "work.go"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "base")

	head := "package main\n\nfunc work() int {\n\ttotal := 0\n\tfor i := 0; i < 10; i++ {\n\t\ttotal += i\n\t}\n\treturn total\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "work.go"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "expand work")

	return dir
}

// gitNumstat returns path -> (added, removed) straight from
// `git diff --numstat` — an oracle the code under test did not choose, so a
// swapped column or a reversed base..head shows up as a mismatch.
func gitNumstat(t *testing.T, dir string, args ...string) map[string][2]int {
	t.Helper()
	full := append([]string{"-C", dir, "diff", "--numstat"}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		t.Fatalf("git diff --numstat %v: %v", args, err)
	}
	res := make(map[string][2]int)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3) //nolint:mnd // numstat: add<TAB>del<TAB>path
		if len(parts) != 3 {
			continue
		}
		add, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		res[parts[2]] = [2]int{add, del}
	}
	return res
}

// TestChangedFiles_AddedRemovedOrientation is the #764 regression test:
// every file's Added/Removed must match `git diff --numstat` column-for-
// column. The fixture is asymmetric (5+/3-) so a swap fails loudly.
func TestChangedFiles_AddedRemovedOrientation(t *testing.T) {
	t.Parallel()
	dir := setupOrientationRepo(t)

	want := gitNumstat(t, dir, "HEAD~1", "HEAD")
	files, err := ChangedFiles(context.Background(), dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) {
		t.Fatalf("expected %d changed files, got %d: %+v", len(want), len(files), files)
	}
	for _, f := range files {
		exp, ok := want[f.Path]
		if !ok {
			t.Fatalf("unexpected path %q in diff", f.Path)
		}
		if exp[0] == exp[1] {
			t.Fatalf("fixture degenerate: %s has equal add/del (%d) — cannot detect a swap", f.Path, exp[0])
		}
		if f.Added != exp[0] || f.Removed != exp[1] {
			t.Errorf("%s: got added=%d removed=%d, numstat says added=%d removed=%d (swapped)",
				f.Path, f.Added, f.Removed, exp[0], exp[1])
		}
	}
}

// TestChangedFiles_StagedOrientation covers the empty-base --cached fallback
// so a swap cannot hide in the second numstat producer either.
func TestChangedFiles_StagedOrientation(t *testing.T) {
	t.Parallel()
	dir := setupOrientationRepo(t)

	staged := "package main\n\nfunc work() int {\n\tx := 1\n\ty := 2\n\tz := 3\n\tw := 4\n\tv := 5\n\treturn x + y + z + w + v\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "work.go"), []byte(staged), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "add", "work.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s: %s", err, out)
	}

	want := gitNumstat(t, dir, "--cached")
	files, err := ChangedFiles(context.Background(), dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		exp, ok := want[f.Path]
		if !ok {
			continue
		}
		if exp[0] == exp[1] {
			t.Fatalf("fixture degenerate: %s has equal add/del (%d)", f.Path, exp[0])
		}
		if f.Added != exp[0] || f.Removed != exp[1] {
			t.Errorf("%s: got added=%d removed=%d, numstat says added=%d removed=%d (swapped)",
				f.Path, f.Added, f.Removed, exp[0], exp[1])
		}
	}
}
