package ingest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// makeCloneDir creates a temp dir with one file, standing in for a clone tree.
func makeCloneDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	clone := filepath.Join(dir, "owner_repo")
	if err := os.MkdirAll(clone, 0o750); err != nil {
		t.Fatalf("mkdir clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, "main.go"), []byte("package main\n"), 0o640); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return clone
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestCloneRef_LastReleaseDeletes proves the refcount is the single delete
// authority: with two holders, the FIRST release must NOT delete the shared dir
// (the other holder is still reading it); only the LAST release deletes it.
// This is the exact code_health-vs-sibling-tool race — a synchronous sibling's
// cleanup must not pull the clone out from under code_health's background read.
func TestCloneRef_LastReleaseDeletes(t *testing.T) {
	dir := makeCloneDir(t)

	// Two readers acquire the same shared clone dir.
	AcquireCloneRef(dir) // background code_health reader
	AcquireCloneRef(dir) // synchronous sibling tool reader

	if got := cloneRefCount(dir); got != 2 {
		t.Fatalf("refcount = %d, want 2 after two acquires", got)
	}

	// Sibling finishes first and releases — dir MUST survive for the other holder.
	if err := ReleaseCloneRef(dir); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if !exists(dir) {
		t.Fatal("clone dir deleted on first release while another holder is live (use-after-delete)")
	}
	if got := cloneRefCount(dir); got != 1 {
		t.Fatalf("refcount = %d, want 1 after first release", got)
	}

	// Background reader finishes — last release deletes the dir.
	if err := ReleaseCloneRef(dir); err != nil {
		t.Fatalf("last release: %v", err)
	}
	if exists(dir) {
		t.Fatal("clone dir still present after last release")
	}
	if got := cloneRefCount(dir); got != 0 {
		t.Fatalf("refcount = %d, want 0 after last release", got)
	}
}

// TestCloneRef_SingleHolderDeletes proves the common case (one reader) still
// deletes on release — the refcount must not leak dirs when uncontended.
func TestCloneRef_SingleHolderDeletes(t *testing.T) {
	dir := makeCloneDir(t)
	AcquireCloneRef(dir)
	if err := ReleaseCloneRef(dir); err != nil {
		t.Fatalf("release: %v", err)
	}
	if exists(dir) {
		t.Fatal("single-holder clone dir not deleted on release")
	}
}

// TestCloneRef_ConcurrentAcquireRelease stresses the refcount under parallel
// acquire/release pairs against one shared dir; the dir must survive while any
// holder is live and be gone once all release. Run with -race.
func TestCloneRef_ConcurrentAcquireRelease(t *testing.T) {
	dir := makeCloneDir(t)

	const holders = 32
	// Acquire all first so the dir is referenced throughout the release storm.
	for range holders {
		AcquireCloneRef(dir)
	}
	if !exists(dir) {
		t.Fatal("dir vanished before any release")
	}

	var wg sync.WaitGroup
	for range holders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ReleaseCloneRef(dir)
		}()
	}
	wg.Wait()

	if exists(dir) {
		t.Fatal("dir present after all holders released")
	}
	if got := cloneRefCount(dir); got != 0 {
		t.Fatalf("refcount = %d, want 0", got)
	}
}
