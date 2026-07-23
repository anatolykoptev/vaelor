package compare_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/compare"
)

// mkCouplingRenameRepo builds a synthetic git repo that reproduces bug #355:
// files change together N times under OLD paths, then are renamed (git mv) to
// NEW paths, then change together once more under the new names. Without
// rename-aware normalization, CollectCoupling keys pairs on the old paths and
// suggest_reviewers (called with current/new paths) sees co-change=0.
//
// Layout:
//   - 3 joint commits touching old/a.go + old/b.go        (co-change=3, old names)
//   - 1 rename commit: git mv old/{a,b}.go new/{a,b}.go   (R100 detected by -M)
//   - 1 joint commit touching new/a.go + new/b.go         (co-change=1, new names)
//   - 3 solo commits touching uncoupled.go alone          (control: 0 partners)
//
// After the fix, the 3 old joint commits are rewritten to new/{a,b}.go, so the
// pair new/a.go <-> new/b.go has CoChanges >= 3 (4 counting the new joint commit).
func mkCouplingRenameRepo(t *testing.T) string {
	t.Helper()
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

	// Seed dirs so git mv has targets.
	for _, d := range []string{"old", "new"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeAdd := func(paths map[string]string) {
		for p, body := range paths {
			if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			run("add", p)
		}
		run("commit", "-m", "joint")
	}

	// 3 joint commits under OLD paths (a.go and b.go change together).
	for i := 0; i < 3; i++ {
		writeAdd(map[string]string{
			"old/a.go": "package old\n// a rev " + string(rune('A'+i)) + "\n",
			"old/b.go": "package old\n// b rev " + string(rune('A'+i)) + "\n",
		})
	}

	// Rename: git mv old -> new (content unchanged => R100 rename detected).
	run("mv", "old/a.go", "new/a.go")
	run("mv", "old/b.go", "new/b.go")
	run("commit", "-m", "rename old -> new")

	// 1 joint commit under NEW paths.
	writeAdd(map[string]string{
		"new/a.go": "package new\n// a rev Z\n",
		"new/b.go": "package new\n// b rev Z\n",
	})

	// Control: a genuinely uncoupled file, touched alone 3 times.
	for i := 0; i < 3; i++ {
		writeAdd(map[string]string{
			"uncoupled.go": "package main\n// solo rev " + string(rune('A'+i)) + "\n",
		})
	}
	return dir
}

// partnersOf returns the co-change partners (other-side file names) for path.
func partnersOf(path string, pairs []compare.CoupledPair) []compare.CoupledPair {
	var out []compare.CoupledPair
	for _, p := range pairs {
		if p.FileA == path || p.FileB == path {
			out = append(out, p)
		}
	}
	return out
}

// TestCollectCoupling_RenameAwareSurfaceCurrentPath is the RED test for bug
// #355: after a rename within the git-log window, CollectCoupling must surface
// the co-change pair under the CURRENT (post-rename) path so that
// suggest_reviewers — which is called with current PR file paths — can match it.
//
// Falsification: revert the rename-normalization fix -> the pair is keyed on
// old/a.go/old/b.go, partnersOf("new/a.go") returns nothing -> RED.
func TestCollectCoupling_RenameAwareSurfaceCurrentPath(t *testing.T) {
	// Runs on the merge gate (not -short-skipped): this is the load-bearing
	// regression guard for #355 — a bugfix whose entire value IS the guard must
	// gate the merge. Git-exec-bound but ~1s, so cheap enough for preflight.
	dir := mkCouplingRenameRepo(t)

	pairs := compare.CollectCoupling(context.Background(), dir, 2)

	// The new-path pair must be present with co-change >= 3 (3 old joint
	// commits rewritten to new names + 1 new joint commit).
	got := partnersOf("new/a.go", pairs)
	if len(got) == 0 {
		t.Fatalf("expected new/a.go to have co-change partners after rename; pairs=%+v", pairs)
	}
	var coMax int
	for _, p := range got {
		if p.CoChanges > coMax {
			coMax = p.CoChanges
		}
		if p.FileA != "new/a.go" && p.FileB != "new/a.go" {
			t.Errorf("partnersOf returned a non-matching pair: %+v", p)
		}
		// The partner must be the renamed sibling, not a stale old/ path.
		other := p.FileB
		if p.FileA == "new/a.go" {
			other = p.FileB
		} else {
			other = p.FileA
		}
		if other != "new/b.go" {
			t.Errorf("expected partner new/b.go, got %q (pair=%+v)", other, p)
		}
	}
	if coMax < 3 {
		t.Errorf("expected co-change >= 3 (3 rewritten old joint commits), got %d", coMax)
	}

	// No pair should reference a stale old/ path — all must resolve to current names.
	for _, p := range pairs {
		if strings.HasPrefix(p.FileA, "old/") || strings.HasPrefix(p.FileB, "old/") {
			t.Errorf("stale pre-rename path in pair (not normalized): %+v", p)
		}
	}
}

// TestCollectCoupling_UncoupledFileStaysZero is the control: a file that
// never co-changes with anything must have zero partners (the fix must not
// fabricate coupling for genuinely independent files).
func TestCollectCoupling_UncoupledFileStaysZero(t *testing.T) {
	// Runs on the merge gate (see the sibling test): the no-fabrication control
	// for #355 belongs on the merge path alongside the guard it balances.
	dir := mkCouplingRenameRepo(t)

	pairs := compare.CollectCoupling(context.Background(), dir, 2)
	if got := partnersOf("uncoupled.go", pairs); len(got) != 0 {
		sort.Slice(got, func(i, j int) bool { return got[i].FileA+got[i].FileB < got[j].FileA+got[j].FileB })
		t.Errorf("uncoupled.go must have 0 partners, got %d: %+v", len(got), got)
	}
}
