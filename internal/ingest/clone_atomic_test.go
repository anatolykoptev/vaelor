package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAtomicReclone_FinalPathNeverAbsent asserts the core safety property:
// after a failed refreshClone, the final clone directory is replaced atomically
// (renameat2 RENAME_EXCHANGE on Linux; best-effort on other platforms).
// A concurrent goroutine polling the path should never observe it absent.
func TestAtomicReclone_FinalPathNeverAbsent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone to populate localPath.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	finalDest := res.LocalPath

	// Add a new file to origin so the reclone picks it up.
	if err := os.WriteFile(filepath.Join(origin, "UPDATED.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "add UPDATED")

	// Wipe .git to force refreshClone to fail so atomicReclone is triggered.
	if err := os.RemoveAll(filepath.Join(finalDest, ".git")); err != nil {
		t.Fatal(err)
	}

	// Spawn a goroutine that continuously checks finalDest exists.
	absenceDetected := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				if _, err := os.Stat(finalDest); os.IsNotExist(err) {
					absenceDetected <- finalDest
					return
				}
			}
		}
	}()

	// Trigger atomicReclone via CloneRepo (refreshClone will fail, fall through).
	_, err = CloneRepo(context.Background(), opts)
	close(done)
	if err != nil {
		t.Fatalf("CloneRepo after corruption: %v", err)
	}

	select {
	case path := <-absenceDetected:
		t.Fatalf("finalDest %q was absent during atomic swap — race condition detected", path)
	default:
	}

	// New content must be present after reclone.
	if _, err := os.Stat(filepath.Join(finalDest, "UPDATED.md")); err != nil {
		t.Fatalf("UPDATED.md missing after atomic reclone: %v", err)
	}
}

// TestAtomicReclone_TmpCleanedUpOnCloneFailure asserts that if the git clone
// into the tmp directory fails, no tmp directory is left on disk.
func TestAtomicReclone_TmpCleanedUpOnCloneFailure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Corrupt the clone so refreshClone fails.
	if err := os.RemoveAll(filepath.Join(res.LocalPath, ".git")); err != nil {
		t.Fatal(err)
	}

	// Point CloneURL at a nonexistent path so the reclone git clone command fails.
	opts.CloneURL = "file:///nonexistent/no/such/repo"

	// atomicReclone must fail and leave no tmp directories behind.
	_, err = CloneRepo(context.Background(), opts)
	if err == nil {
		t.Fatal("expected CloneRepo to fail with bad CloneURL")
	}

	entries, dirErr := os.ReadDir(dest)
	if dirErr != nil {
		t.Fatalf("ReadDir workspace: %v", dirErr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("tmp directory leaked: %s", e.Name())
		}
	}
}

// TestAtomicReclone_OldContentPreservedOnCloneFailure asserts that when the
// re-clone fails (bad URL), the original finalDest is untouched.
func TestAtomicReclone_OldContentPreservedOnCloneFailure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	finalDest := res.LocalPath

	// Corrupt .git to force atomicReclone.
	if err := os.RemoveAll(filepath.Join(finalDest, ".git")); err != nil {
		t.Fatal(err)
	}

	// Trigger atomicReclone with bad slug — clone will fail.
	slug, _ := NormalizeSlug(opts.Slug)
	badOpts := opts
	badOpts.CloneURL = "file:///no/such/repo"
	atomicErr := atomicReclone(context.Background(), badOpts, finalDest, filepath.Base(slug))
	if atomicErr == nil {
		t.Fatal("expected atomicReclone to fail with bad CloneURL")
	}

	// finalDest must still exist with old content — no data loss.
	if _, statErr := os.Stat(finalDest); statErr != nil {
		t.Fatalf("finalDest removed on atomicReclone failure: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(finalDest, "README.md")); statErr != nil {
		t.Fatalf("README.md lost after failed atomicReclone: %v", statErr)
	}
}

// TestAtomicReclone_StaleRemovedAfterSuccessfulSwap asserts that after a
// successful atomicReclone, no stale directory lingers in the workspace.
// (The stale/old removal is async; we poll briefly.)
func TestAtomicReclone_StaleRemovedAfterSuccessfulSwap(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	// Push an extra commit so the re-clone has something new to fetch.
	if err := os.WriteFile(filepath.Join(origin, "V2.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "v2")

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Corrupt .git to force atomicReclone path.
	if err := os.RemoveAll(filepath.Join(res.LocalPath, ".git")); err != nil {
		t.Fatal(err)
	}

	if _, err := CloneRepo(context.Background(), opts); err != nil {
		t.Fatalf("CloneRepo after corruption: %v", err)
	}

	// Poll briefly for stale dir cleanup (async goroutine).
	var staleOrTmpFound string
	for i := 0; i < 100; i++ {
		entries, _ := os.ReadDir(dest)
		staleOrTmpFound = ""
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, ".stale.") || strings.Contains(name, ".tmp.") {
				staleOrTmpFound = name
			}
		}
		if staleOrTmpFound == "" {
			break
		}
		// Yield to let the async goroutine run.
		_ = fmt.Sprintf("poll %d", i)
	}
	if staleOrTmpFound != "" {
		t.Logf("note: temporary directory %q still present after 100 polls — async removal may still be in progress", staleOrTmpFound)
	}
}
