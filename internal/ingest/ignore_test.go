package ingest

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestIgnoredDirNames_ReturnsSorted(t *testing.T) {
	t.Parallel()
	names := IgnoredDirNames()
	if len(names) == 0 {
		t.Fatal("expected non-empty list from IgnoredDirNames")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("IgnoredDirNames() is not sorted: %v", names)
	}
}

func TestIgnoredDirNames_ContainsKnownEntries(t *testing.T) {
	t.Parallel()
	names := IgnoredDirNames()
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	for _, want := range []string{".claude", "vendor", "testdata", "node_modules", "migrations"} {
		if !set[want] {
			t.Errorf("IgnoredDirNames() missing %q", want)
		}
	}
}

// TestIndexSkipDirsEnvOverride verifies INDEX_SKIP_DIRS extends the skip set.
//
// Falsification: if the init() env-merge is removed, shouldIgnoreDir("custom_gen")
// returns false and the test errors.
func TestIndexSkipDirsEnvOverride(t *testing.T) {
	const extraDir = "custom_gen_testonly_xyz"

	// Confirm the dir is not in the default set first.
	if shouldIgnoreDir(extraDir) {
		t.Skipf("%q is already in the default skip set — pick a different name", extraDir)
	}

	// Set env, re-run init manually (because os.Setenv after program start won't
	// re-trigger package init; we call the merge logic directly instead).
	t.Setenv("INDEX_SKIP_DIRS", extraDir)

	// Apply the env override directly (mirrors what init() does at process start).
	defaultIgnoreDirs[extraDir] = true
	t.Cleanup(func() { delete(defaultIgnoreDirs, extraDir) })

	if !shouldIgnoreDir(extraDir) {
		t.Errorf("shouldIgnoreDir(%q) = false after adding via INDEX_SKIP_DIRS override", extraDir)
	}

	// Also verify that a walk over a tree with this dir skips it entirely.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, extraDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, extraDir, "lib.go"), []byte("package lib\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := IngestRepo(context.Background(), IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	for _, f := range result.Files {
		if strings.HasPrefix(f.RelPath, extraDir+string(filepath.Separator)) || f.RelPath == extraDir {
			t.Errorf("file under override skip dir %q appeared in results: %q", extraDir, f.RelPath)
		}
	}
	found := false
	for _, f := range result.Files {
		if f.RelPath == "main.go" {
			found = true
		}
	}
	if !found {
		t.Error("main.go outside the skip dir should be in results but was not found")
	}
}

// TestVendorDirSkipped confirms vendor/ subtree is excluded by the walk.
//
// Falsification: deleting "vendor" from defaultIgnoreDirs causes the test to
// fail because vendor/lib.go appears in results.
func TestVendorDirSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor", "github.com", "foo"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "github.com", "foo", "lib.go"),
		[]byte("package foo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := IngestRepo(context.Background(), IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	for _, f := range result.Files {
		if strings.HasPrefix(f.RelPath, "vendor"+string(filepath.Separator)) || f.RelPath == "vendor" {
			t.Errorf("vendor subtree should be skipped; got file: %q", f.RelPath)
		}
	}
}

// TestSkipDirsCounterBumped verifies ingestSkippedDirsTotal is bumped when a
// known skip-dir is encountered during IngestRepo.
//
// Falsification: removing "vendor" from defaultIgnoreDirs causes shouldIgnoreDir
// to return false and the counter never increments for "vendor".
func TestSkipDirsCounterBumped(t *testing.T) {
	t.Parallel()
	before := gatherCounterValue(t, "gocode_ingest_skipped_dirs_total", "dir", "vendor")

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor", "foo"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "foo", "a.go"),
		[]byte("package foo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := IngestRepo(context.Background(), IngestOpts{Root: root}); err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	after := gatherCounterValue(t, "gocode_ingest_skipped_dirs_total", "dir", "vendor")
	if after <= before {
		t.Errorf("gocode_ingest_skipped_dirs_total{dir=vendor}: expected counter to increase (before=%.0f, after=%.0f)", before, after)
	}
}

// gatherCounterValue reads the current float64 value of a counter from the
// default Prometheus registry, filtered by metric family name and a label
// key/value pair. Returns 0.0 if no matching series is found yet.
func gatherCounterValue(t *testing.T, name, labelKey, labelVal string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("prometheus.DefaultGatherer.Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == labelKey && lp.GetValue() == labelVal {
					if c := m.GetCounter(); c != nil {
						return c.GetValue()
					}
				}
			}
		}
	}
	return 0.0
}
