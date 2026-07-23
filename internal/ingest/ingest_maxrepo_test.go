package ingest

import (
	"context"
	"path/filepath"
	"testing"
)

// TestIngestRepo_MaxRepoBytesCapsTotalSize proves the MaxRepoBytes field in
// IngestOpts changes behavior: when set, ingestion stops accepting files once
// the cumulative ingested size exceeds the cap. Without the cap all files are
// ingested; with a cap smaller than the total, fewer files are kept and a
// "repo_oversize" skip reason is recorded.
//
// Falsification: revert the MaxRepoBytes enforcement in IngestRepo → the
// capped run ingests the same files as the uncapped run → the assertion that
// the capped run has strictly fewer files (and a non-zero repo_oversize tally)
// goes RED.
func TestIngestRepo_MaxRepoBytesCapsTotalSize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Three Go files of distinct sizes so the cap lands between file 1 and 2.
	writeFile(t, filepath.Join(root, "a.go"), "package main // a\n")      // ~17 bytes
	writeFile(t, filepath.Join(root, "b.go"), "package main // bbb\n")    // ~19 bytes
	writeFile(t, filepath.Join(root, "c.go"), "package main // cccccc\n") // ~21 bytes

	// Baseline: no cap → all three ingested.
	full, err := IngestRepo(context.Background(), IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo (no cap): %v", err)
	}
	if len(full.Files) != 3 {
		t.Fatalf("baseline ingested %d files, want 3", len(full.Files))
	}

	// Cap just above file a.go's size but below the cumulative total → must
	// stop after the first file and record the repo_oversize reason.
	capped, err := IngestRepo(context.Background(), IngestOpts{
		Root:         root,
		MaxRepoBytes: full.Files[0].Size + 1, // a.go fits, b.go would exceed
	})
	if err != nil {
		t.Fatalf("IngestRepo (capped): %v", err)
	}
	if len(capped.Files) >= len(full.Files) {
		t.Errorf("capped ingested %d files, want strictly fewer than %d (cap not enforced)",
			len(capped.Files), len(full.Files))
	}
	if capped.SkippedReasons["repo_oversize"] == 0 {
		t.Errorf("capped run recorded 0 repo_oversize skips, want >0 (cap enforced but reason not tallied)")
	}
}
