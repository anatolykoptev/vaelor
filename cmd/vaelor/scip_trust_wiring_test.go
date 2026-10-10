package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The production wiring (registerTools -> scipTrustedRoots) must not trust an
// auto-index dir that contains the clone workspace or the temp dir.
func TestScipTrustedRoots_DropsOverlaps(t *testing.T) {
	base := t.TempDir()
	good := filepath.Join(base, "good")
	withWorkspace := filepath.Join(base, "withws")
	workspace := filepath.Join(withWorkspace, "clones")
	for _, d := range []string{good, workspace} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// Keep os.TempDir() disjoint from the fixture (t.TempDir lives under it).
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "sys-tmp"))
	if err := os.MkdirAll(os.TempDir(), 0o750); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AutoIndexDirs: []string{good, withWorkspace}, WorkspaceDir: workspace}
	if got := scipTrustedRoots(cfg); !slices.Equal(got, []string{good}) {
		t.Errorf("WORKSPACE_DIR inside a trusted dir: got %v, want only %v", got, good)
	}

	// A trusted dir that contains os.TempDir() (PR worktrees) is dropped too.
	t.Setenv("TMPDIR", filepath.Join(good, "tmp"))
	if err := os.MkdirAll(filepath.Join(good, "tmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	cfg = Config{AutoIndexDirs: []string{good}, WorkspaceDir: filepath.Join(base, "elsewhere")}
	if got := scipTrustedRoots(cfg); len(got) != 0 {
		t.Errorf("TempDir inside a trusted dir: got %v, want none", got)
	}
}
