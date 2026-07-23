package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRepoContentHash_SizeSensitive verifies that two files identical in their
// first 4KB but differing in SIZE produce different repo content hashes.
// This is the #592 review MAJOR (correctness) fix: the old hash folded only
// {relpath, first-4KB-sha} — an in-place edit past byte 4096, or a
// truncate/append leaving the first 4KB intact, yielded the SAME hash, so a
// stale graph was judged fresh. Folding size catches append/truncate/tail-edits
// cheaply (size comes from DirEntry.Info, no extra content read).
//
// RED before the fix: RepoContentHash folds only the first-4KB digest; two
// files with identical first 4KB but different sizes produce the same hash →
// test fails. GREEN after: size is folded into each entry's digest.
func TestRepoContentHash_SizeSensitive(t *testing.T) {
	t.Parallel()

	// Build a 8KB payload whose first 4KB are identical across both files;
	// only the tail differs (simulates an append/edit past byte 4096).
	head := make([]byte, 4096)
	for i := range head {
		head[i] = byte(i % 256)
	}

	dirA := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "big.go"), append(append([]byte{}, head...), []byte("AAAA-tail")...), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}

	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "big.go"), append(append([]byte{}, head...), []byte("BBBB-longer-tail")...), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}

	hA := RepoContentHash(dirA)
	hB := RepoContentHash(dirB)
	if hA == hB {
		t.Fatalf("RepoContentHash not size-sensitive: two files identical in first 4KB but different sizes produced the same hash %q", hA)
	}
}

// TestRepoContentHash_TruncateDetected verifies that truncating a file (leaving
// the first 4KB intact) changes the hash — the append/truncate half of the
// MAJOR (correctness) fix.
func TestRepoContentHash_TruncateDetected(t *testing.T) {
	t.Parallel()

	head := make([]byte, 4096)
	for i := range head {
		head[i] = byte(i % 256)
	}

	dir := t.TempDir()
	fpath := filepath.Join(dir, "big.go")
	if err := os.WriteFile(fpath, append(append([]byte{}, head...), []byte("tail-bytes-that-make-it-8KB-ish-extra-content")...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hBefore := RepoContentHash(dir)

	// Truncate to 4KB — first 4KB (the only chunk the old hash read) are
	// unchanged, but the size drops.
	if err := os.Truncate(fpath, 4096); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	hAfter := RepoContentHash(dir)

	if hBefore == hAfter {
		t.Fatalf("RepoContentHash did not detect truncate: hash unchanged %q after truncating a file whose first 4KB were preserved", hBefore)
	}
}

// TestRepoContentHash_IgnoresIgnoreDirs verifies that files under an
// ignore-dir (node_modules) do NOT affect the repo content hash. This is the
// #592 review MAJOR (perf+correctness) fix: the old walk skipped only
// dotfiles, so vendored/node_modules files drove source-graph freshness and
// were opened+read on every AGE tool call. Reusing ingest's defaultIgnoreDirs
// means vendored files no longer drive freshness.
//
// RED before the fix: RepoContentHash descends into node_modules, so adding a
// file there changes the hash → test fails. GREEN after: node_modules is
// skipped, hash is stable.
func TestRepoContentHash_IgnoresIgnoreDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	hBefore := RepoContentHash(dir)

	// Add a file under node_modules — must not change the hash.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("module.exports = 1\n"), 0o644); err != nil {
		t.Fatalf("write node_modules file: %v", err)
	}
	// Also add under vendor — another defaultIgnoreDirs entry.
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "v.go"), []byte("package vendor\n"), 0o644); err != nil {
		t.Fatalf("write vendor file: %v", err)
	}

	hAfter := RepoContentHash(dir)
	if hBefore != hAfter {
		t.Fatalf("RepoContentHash changed when files were added under ignore-dirs (node_modules/vendor): %q -> %q — ignore-dirs must not drive source-graph freshness", hBefore, hAfter)
	}
}

// TestRepoContentHash_StableAcrossCalls verifies the hash is deterministic for
// an unchanged tree (regression guard for the size/ignore-dir changes — a
// stable repo must still produce a stable hash).
func TestRepoContentHash_StableAcrossCalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc a() {}\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\nfunc b() {}\n"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	h1 := RepoContentHash(dir)
	h2 := RepoContentHash(dir)
	if h1 != h2 {
		t.Fatalf("RepoContentHash not deterministic for unchanged tree: %q vs %q", h1, h2)
	}
}
