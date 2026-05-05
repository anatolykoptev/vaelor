package explore

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// cloneShallow makes a file-protocol shallow clone of src into dst with the
// given depth.  t.Fatal is called on any error.
func cloneShallow(t *testing.T, src, dst string, depth int) {
	t.Helper()
	out, err := exec.Command("git", "clone",
		"--depth", fmt.Sprint(depth),
		"file://"+src, dst,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --depth %d: %v: %s", depth, err, out)
	}
}

// exploreCounterValue reads the current value of the named counter for the
// given label set from the default Prometheus registry.  Returns 0 when no
// sample has been written yet.
func exploreCounterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if exploreMatchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func exploreMatchLabels(m *dto.Metric, want map[string]string) bool {
	have := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		have[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

const metricFilesChangedMethod = "gocode_explore_files_changed_method_total"

// TestCountDiffTreeFilesMetric_DiffTree checks that a normal commit increments
// the diff_tree method counter.
func TestCountDiffTreeFilesMetric_DiffTree(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	// Create a second commit so it has a parent (normal diff-tree path).
	commitFiles(t, dir, "first", map[string]string{"a.go": "package main\n"})
	commitFiles(t, dir, "second", map[string]string{"b.go": "package main\n"})

	// Get the SHA of the second (non-initial) commit.
	sha := latestSHA(t, dir)

	before := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "diff_tree"})
	_, err := countDiffTreeFiles(context.Background(), dir, sha)
	if err != nil {
		t.Fatalf("countDiffTreeFiles: %v", err)
	}
	after := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "diff_tree"})
	if after-before != 1 {
		t.Errorf("diff_tree counter delta = %v, want 1", after-before)
	}
}

// TestCountDiffTreeFilesMetric_RootFallback checks that the initial commit
// triggers the root_fallback counter.
func TestCountDiffTreeFilesMetric_RootFallback(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	// Only one commit — the initial commit triggers the --root retry path.
	commitFiles(t, dir, "init", map[string]string{"a.go": "package main\n"})

	sha := latestSHA(t, dir)

	before := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})
	_, err := countDiffTreeFiles(context.Background(), dir, sha)
	if err != nil {
		t.Fatalf("countDiffTreeFiles: %v", err)
	}
	after := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})
	if after-before != 1 {
		t.Errorf("root_fallback counter delta = %v, want 1", after-before)
	}
}

// TestCountDiffTreeFilesMetric_Error checks that a bad SHA increments the
// error counter.
func TestCountDiffTreeFilesMetric_Error(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	commitFiles(t, dir, "init", map[string]string{"a.go": "package main\n"})

	before := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "error"})
	_, err := countDiffTreeFiles(context.Background(), dir, "deadbeef000")
	if err == nil {
		t.Fatal("expected error for invalid SHA")
	}
	after := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "error"})
	if after-before != 1 {
		t.Errorf("error counter delta = %v, want 1", after-before)
	}
}

// latestSHA returns the full SHA of HEAD in the given git repo.
func latestSHA(t *testing.T, dir string) string {
	t.Helper()
	raw, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	out := string(raw)
	// Trim trailing newline.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}

// TestCountDiffTreeFiles_ShallowClone_Depth1 is the regression test for the
// prod bug: with --depth=1 every commit looks like the shallow boundary because
// there is no parent object.  countDiffTreeFiles must return 0 (not the
// all-files count) and increment the shallow_boundary metric.
//
// Empirical evidence: before this fix, `explore` on anatolykoptev/go-code
// reported files_changed=2397 for a 6-file squash commit (c995fdb) because
// the --root fallback fired against the empty tree and counted every file.
func TestCountDiffTreeFiles_ShallowClone_Depth1(t *testing.T) {
	// Build a source repo with 3 commits touching different files.
	src := t.TempDir()
	initGitRepo(t, src)
	commitFiles(t, src, "first", map[string]string{"a.go": "package main\n"})
	commitFiles(t, src, "second", map[string]string{"b.go": "package main\n"})
	commitFiles(t, src, "third", map[string]string{"c.go": "package main\n"})

	// Shallow clone with depth=1: no commit has a visible parent.
	dst := t.TempDir()
	cloneShallow(t, src, dst, 1)

	sha := latestSHA(t, dst)
	t.Logf("shallow-depth-1 HEAD sha=%s", sha)

	beforeBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})
	beforeRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})
	count, err := countDiffTreeFiles(context.Background(), dst, sha)
	if err != nil {
		t.Fatalf("countDiffTreeFiles: %v", err)
	}
	afterBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})
	afterRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})

	t.Logf("shallow clone (depth=1) HEAD diff-tree -r returns %d files (want 0, not all-files count)", count)

	// Must return 0, not the total file count of the repo.
	if count != 0 {
		t.Errorf("shallow boundary: count = %d, want 0 (total files in repo would be 3)", count)
	}
	if afterBoundary-beforeBoundary != 1 {
		t.Errorf("shallow_boundary counter delta = %v, want 1", afterBoundary-beforeBoundary)
	}
	// root_fallback must NOT fire for a true shallow boundary.
	if afterRootFallback != beforeRootFallback {
		t.Errorf("root_fallback must not fire for shallow boundary, delta = %v", afterRootFallback-beforeRootFallback)
	}
}

// TestCountDiffTreeFiles_ShallowClone_Depth2 verifies that with depth=2 (the
// fixed clone depth) non-initial commits have a visible parent and diff-tree
// returns the correct per-commit file count, not 0 and not all-files.
func TestCountDiffTreeFiles_ShallowClone_Depth2(t *testing.T) {
	// Build a source repo: init + 3 data commits, each touching exactly 1 file.
	src := t.TempDir()
	initGitRepo(t, src)
	commitFiles(t, src, "seed", map[string]string{"seed.go": "package main\n"})
	commitFiles(t, src, "first", map[string]string{"a.go": "package main\n"})
	commitFiles(t, src, "second", map[string]string{"b.go": "package main\n"})
	commitFiles(t, src, "third", map[string]string{"c.go": "package main\n"})

	// Shallow clone with depth=2: HEAD has its parent available.
	dst := t.TempDir()
	cloneShallow(t, src, dst, 2)

	sha := latestSHA(t, dst)
	t.Logf("shallow-depth-2 HEAD sha=%s", sha)

	beforeDiffTree := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "diff_tree"})
	beforeBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})
	beforeRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})

	count, err := countDiffTreeFiles(context.Background(), dst, sha)
	if err != nil {
		t.Fatalf("countDiffTreeFiles: %v", err)
	}

	afterDiffTree := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "diff_tree"})
	afterBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})
	afterRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})

	t.Logf("shallow clone (depth=2) HEAD diff-tree -r returns %d files (want 1)", count)

	// HEAD ("third") touches exactly 1 file.
	const wantFiles = 1
	if count != wantFiles {
		t.Errorf("depth=2 HEAD: count = %d, want %d (must use real diff, not all-files or 0)", count, wantFiles)
	}
	if afterDiffTree-beforeDiffTree != 1 {
		t.Errorf("diff_tree counter delta = %v, want 1", afterDiffTree-beforeDiffTree)
	}
	if afterBoundary != beforeBoundary {
		t.Errorf("shallow_boundary counter should not increment for depth=2, delta = %v", afterBoundary-beforeBoundary)
	}
	// root_fallback must not fire for a non-initial commit with a visible parent.
	if afterRootFallback != beforeRootFallback {
		t.Errorf("root_fallback must not fire for depth=2 non-initial commit, delta = %v", afterRootFallback-beforeRootFallback)
	}
}

// TestCountDiffTreeFiles_ShallowClone_SingleCommitRepo is the regression test
// for the edge case where a shallow clone happens to contain exactly 1 commit
// (because the repo itself has only 1 commit total).  isShallowBoundary must
// return false in this case so that the --root fallback fires and the actual
// file count is returned instead of 0.
func TestCountDiffTreeFiles_ShallowClone_SingleCommitRepo(t *testing.T) {
	// Source repo with exactly 1 commit touching 4 files.
	src := t.TempDir()
	initGitRepo(t, src)
	commitFiles(t, src, "initial", map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n",
		"c.go": "package main\n",
		"d.go": "package main\n",
	})

	// Shallow clone with depth=1 — the clone is shallow yet contains the true
	// root of the repo (single commit history).
	dst := t.TempDir()
	cloneShallow(t, src, dst, 1)

	sha := latestSHA(t, dst)
	t.Logf("single-commit shallow clone HEAD sha=%s", sha)

	beforeRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})
	beforeBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})

	count, err := countDiffTreeFiles(context.Background(), dst, sha)
	if err != nil {
		t.Fatalf("countDiffTreeFiles: %v", err)
	}

	afterRootFallback := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "root_fallback"})
	afterBoundary := exploreCounterValue(t, metricFilesChangedMethod, map[string]string{"method": "shallow_boundary"})

	t.Logf("single-commit shallow clone: countDiffTreeFiles returned %d (want 4)", count)

	const wantFiles = 4
	if count != wantFiles {
		t.Errorf("single-commit shallow clone: count = %d, want %d", count, wantFiles)
	}
	// root_fallback must fire because this is a true initial commit.
	if afterRootFallback-beforeRootFallback != 1 {
		t.Errorf("root_fallback counter delta = %v, want 1", afterRootFallback-beforeRootFallback)
	}
	// shallow_boundary must NOT fire — this is the true root, not a truncation.
	if afterBoundary != beforeBoundary {
		t.Errorf("shallow_boundary must not fire for true initial commit, delta = %v", afterBoundary-beforeBoundary)
	}
}
