package ingest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// TestWalkFilesFromEnv covers MAX_WALK_FILES parsing: unset/invalid values
// keep the default, a positive integer overrides it.
//
// Not parallel: t.Setenv forbids it.
func TestWalkFilesFromEnv(t *testing.T) {
	cases := []struct {
		val  string
		want int
	}{
		{"", defaultMaxWalkFiles},
		{"  ", defaultMaxWalkFiles},
		{"abc", defaultMaxWalkFiles},
		{"0", defaultMaxWalkFiles},
		{"-5", defaultMaxWalkFiles},
		{"150000", 150000},
		{" 42 ", 42},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q", c.val), func(t *testing.T) {
			t.Setenv("MAX_WALK_FILES", c.val)
			if got := walkFilesFromEnv(); got != c.want {
				t.Errorf("walkFilesFromEnv() with MAX_WALK_FILES=%q = %d, want %d", c.val, got, c.want)
			}
		})
	}
}

// TestIngestRepo_MaxWalkFilesCapsWalk proves the walk cap is read from
// maxWalkFiles (the MAX_WALK_FILES-derived value) rather than a constant:
// lowering it truncates the walk and tallies "max_files" skips.
//
// Falsification: make maxWalkFiles a constant again (or ignore it in
// effectiveMaxFiles) → the lowered cap has no effect → all 5 files are
// ingested and the assertions go RED.
//
// Not parallel: mutates the package-level maxWalkFiles.
func TestIngestRepo_MaxWalkFilesCapsWalk(t *testing.T) {
	orig := maxWalkFiles
	t.Cleanup(func() { maxWalkFiles = orig })

	root := t.TempDir()
	for i := range 5 {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%d.go", i)), "package main\n")
	}

	maxWalkFiles = 3
	res, err := IngestRepo(context.Background(), IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	if len(res.Files) != 3 {
		t.Errorf("ingested %d files with maxWalkFiles=3, want 3", len(res.Files))
	}
	if res.SkippedReasons["max_files"] != 2 {
		t.Errorf("max_files skips = %d, want 2", res.SkippedReasons["max_files"])
	}
}
