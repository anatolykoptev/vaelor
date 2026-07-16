package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestIngestRepoCache_HitOnSecondCall verifies that a second IngestRepo
// call against the same repo with the same opts returns the cached result
// without re-walking the filesystem. We detect a cache hit by checking
// that the "ingest: starting repo walk" log line is NOT emitted on the
// second call — but since we can't easily capture slog output, we instead
// verify correctness: the second call returns the same *IngestResult
// pointer (proving it came from the cache, not a fresh walk).
func TestIngestRepoCache_HitOnSecondCall(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := IngestOpts{Root: dir}

	r1, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first IngestRepo: %v", err)
	}
	if len(r1.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r1.Files))
	}

	r2, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second IngestRepo: %v", err)
	}

	// Cache hit: same pointer (not a fresh allocation).
	if r1 != r2 {
		t.Error("expected same *IngestResult pointer on cache hit; got different pointers")
	}
}

// TestIngestRepoCache_InvalidateOnContentChange verifies that modifying a
// file after the first call invalidates the cache, so the second call
// re-walks and returns a fresh result.
func TestIngestRepoCache_InvalidateOnContentChange(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)

	dir := t.TempDir()
	fpath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(fpath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := IngestOpts{Root: dir}

	r1, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first IngestRepo: %v", err)
	}
	if len(r1.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r1.Files))
	}

	// Add a new file — content hash changes.
	if err := os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main\nfunc util() {}\n"), 0o644); err != nil {
		t.Fatalf("write util.go: %v", err)
	}

	r2, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second IngestRepo: %v", err)
	}

	// Cache miss: fresh result with 2 files.
	if len(r2.Files) != 2 {
		t.Errorf("expected 2 files after content change (cache invalidated), got %d", len(r2.Files))
	}
}

// TestIngestRepoCache_DifferentOptsDifferentEntries verifies that two
// calls with different IngestOpts (e.g. different Focus) produce separate
// cache entries and don't collide.
func TestIngestRepoCache_DifferentOptsDifferentEntries(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "util.go"), []byte("package pkg\nfunc util() {}\n"), 0o644); err != nil {
		t.Fatalf("write pkg/util.go: %v", err)
	}

	// No focus: all files.
	r1, err := IngestRepo(context.Background(), IngestOpts{Root: dir})
	if err != nil {
		t.Fatalf("IngestRepo no focus: %v", err)
	}
	if len(r1.Files) != 2 {
		t.Fatalf("expected 2 files without focus, got %d", len(r1.Files))
	}

	// Focus on pkg/: only pkg/util.go.
	r2, err := IngestRepo(context.Background(), IngestOpts{Root: dir, Focus: "pkg/"})
	if err != nil {
		t.Fatalf("IngestRepo focus=pkg/: %v", err)
	}
	if len(r2.Files) != 1 {
		t.Errorf("expected 1 file with focus=pkg/, got %d", len(r2.Files))
	}

	// No focus again: should hit cache (same pointer as r1).
	r3, err := IngestRepo(context.Background(), IngestOpts{Root: dir})
	if err != nil {
		t.Fatalf("IngestRepo no focus (cached): %v", err)
	}
	if r1 != r3 {
		t.Error("expected cache hit for same opts (no focus); got different pointer")
	}
}

// TestIngestRepoCache_DifferentRootsDifferentEntries verifies that two
// repos with the same opts but different roots don't collide.
func TestIngestRepoCache_DifferentRootsDifferentEntries(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir1, "a.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write dir1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "b.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write dir2: %v", err)
	}

	r1, err := IngestRepo(context.Background(), IngestOpts{Root: dir1})
	if err != nil {
		t.Fatalf("dir1: %v", err)
	}
	r2, err := IngestRepo(context.Background(), IngestOpts{Root: dir2})
	if err != nil {
		t.Fatalf("dir2: %v", err)
	}

	if r1.Root == r2.Root {
		t.Errorf("expected different roots, got same %q", r1.Root)
	}
	if len(r1.Files) != 1 || r1.Files[0].RelPath != "a.go" {
		t.Errorf("dir1: expected a.go, got %v", r1.Files)
	}
	if len(r2.Files) != 1 || r2.Files[0].RelPath != "b.go" {
		t.Errorf("dir2: expected b.go, got %v", r2.Files)
	}
}
