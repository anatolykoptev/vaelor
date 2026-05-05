package explore

import (
	"context"
	"os/exec"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

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
