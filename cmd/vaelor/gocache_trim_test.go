package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	fakeCacheOldAge   = 96 * time.Hour
	fakeCacheFreshAge = time.Hour
)

// writeCacheEntry writes size bytes at path and back-dates its mtime by age,
// mirroring how the go tool leaves unused entries cold (mtime = last use).
func writeCacheEntry(t *testing.T, path string, size int64, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mtime := time.Now().Add(-age)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// fakeGoCache builds a GOCACHE-shaped temp dir: bookkeeping files at the root
// (README, trim.txt — old but never eligible), two-hex entry subdirs mixing
// stale and fresh entries, non-hex dirs, a nested dir inside a hex subdir,
// and a symlink inside a hex subdir whose target lives outside the cache.
func fakeGoCache(t *testing.T) (dir, outsideTarget string) {
	t.Helper()
	dir = t.TempDir()
	writeCacheEntry(t, filepath.Join(dir, "README"), 64, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "trim.txt"), 16, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", "aa11bb22-d"), 100, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", "cc33dd44-d"), 50, fakeCacheFreshAge)
	writeCacheEntry(t, filepath.Join(dir, "0f", "ff00ff00-s"), 200, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "zz", "not-hex"), 30, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "abc", "three-chars"), 30, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", "nested", "deep-d"), 30, fakeCacheOldAge)

	outside := t.TempDir()
	outsideTarget = filepath.Join(outside, "victim")
	writeCacheEntry(t, outsideTarget, 30, fakeCacheOldAge)
	if err := os.Symlink(outsideTarget, filepath.Join(dir, "ab", "ee55-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return dir, outsideTarget
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to survive the trim: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted, stat err=%v", path, err)
	}
}

// Stale entries inside two-hex subdirs are deleted; fresh entries survive;
// the returned counts and bytes are exact.
//
// Mutation that must turn it RED: invert the age comparison in
// trimGoCacheSubdir (keep-stale/delete-fresh) — the fresh entry is then gone
// and the stale ones remain.
func TestTrimGoCacheDir_EvictsOnlyStaleHexEntries(t *testing.T) {
	dir, _ := fakeGoCache(t)

	st := trimGoCacheDir(dir, 48*time.Hour, time.Now())

	if st.errs != 0 || st.firstErr != nil {
		t.Fatalf("expected a clean run, got errs=%d firstErr=%v", st.errs, st.firstErr)
	}
	if st.files != 2 || st.bytes != 300 {
		t.Fatalf("expected 2 files / 300 bytes removed, got %d files / %d bytes", st.files, st.bytes)
	}
	assertMissing(t, filepath.Join(dir, "ab", "aa11bb22-d"))
	assertMissing(t, filepath.Join(dir, "0f", "ff00ff00-s"))
	assertExists(t, filepath.Join(dir, "ab", "cc33dd44-d"))
}

// Root files, non-hex subdirs, nested dirs inside hex subdirs and symlinks
// must all survive regardless of age.
//
// Mutation that must turn it RED: drop the two-hex-subdirectory restriction
// in trimGoCacheDir — root files (README, trim.txt) then become eligible and
// disappear.
func TestTrimGoCacheDir_NeverTouchesRootOrNonHex(t *testing.T) {
	dir, outsideTarget := fakeGoCache(t)

	_ = trimGoCacheDir(dir, 48*time.Hour, time.Now())

	for _, p := range []string{
		filepath.Join(dir, "README"),
		filepath.Join(dir, "trim.txt"),
		filepath.Join(dir, "zz", "not-hex"),
		filepath.Join(dir, "abc", "three-chars"),
		filepath.Join(dir, "ab", "nested"),
		filepath.Join(dir, "ab", "nested", "deep-d"),
		outsideTarget,
	} {
		assertExists(t, p)
	}
	if _, err := os.Lstat(filepath.Join(dir, "ab", "ee55-link")); err != nil {
		t.Fatalf("symlink entry must not be followed or deleted: %v", err)
	}
}

// An empty cache dir is a no-op, and a missing dir never reaches the trim —
// it is dropped at resolution.
func TestTrimGoCacheDir_EmptyAndMissing_AreNoOps(t *testing.T) {
	st := trimGoCacheDir(t.TempDir(), 48*time.Hour, time.Now())
	if st.files != 0 || st.bytes != 0 || st.errs != 0 || st.firstErr != nil {
		t.Fatalf("empty dir must be a no-op, got %+v", st)
	}
	missing := filepath.Join(t.TempDir(), "gone")
	if got := filterGoCacheDirs([]string{missing}); len(got) != 0 {
		t.Fatalf("missing dir must be dropped at resolution, got %v", got)
	}
}

// The Prometheus counters are bumped with the exact aggregate of the run —
// the production signal a silent or wrong trim would otherwise lose.
func TestTrimGoCacheDirs_BumpsCountersWithExactTotals(t *testing.T) {
	dir, _ := fakeGoCache(t)
	beforeF := testutil.ToFloat64(goCacheTrimFilesTotal)
	beforeB := testutil.ToFloat64(goCacheTrimBytesTotal)
	beforeE := testutil.ToFloat64(goCacheTrimErrorsTotal)

	trimGoCacheDirs([]string{dir}, 48*time.Hour)

	if d := testutil.ToFloat64(goCacheTrimFilesTotal) - beforeF; d != 2 {
		t.Fatalf("vaelor_gocache_trim_files_total delta = %v, want 2", d)
	}
	if d := testutil.ToFloat64(goCacheTrimBytesTotal) - beforeB; d != 300 {
		t.Fatalf("vaelor_gocache_trim_bytes_total delta = %v, want 300", d)
	}
	if d := testutil.ToFloat64(goCacheTrimErrorsTotal) - beforeE; d != 0 {
		t.Fatalf("vaelor_gocache_trim_errors_total delta = %v, want 0", d)
	}
}

// "off" (go's cache-disabled sentinel), blanks, missing paths, files and
// duplicate spellings of the same dir are all dropped.
func TestFilterGoCacheDirs(t *testing.T) {
	real1, real2 := t.TempDir(), t.TempDir()
	notDir := filepath.Join(t.TempDir(), "a-file")
	writeCacheEntry(t, notDir, 1, time.Hour)

	got := filterGoCacheDirs([]string{
		"off",
		"",
		"   ",
		filepath.Join(t.TempDir(), "missing"),
		notDir,
		real1,
		real1 + string(os.PathSeparator), // same dir, different spelling
		real2,
	})

	if len(got) != 2 || got[0] != real1 || got[1] != real2 {
		t.Fatalf("expected [%s %s], got %v", real1, real2, got)
	}
}

// VAELOR_GOCACHE_MAX_AGE overrides the default; invalid, non-positive and
// empty values fall back to it.
func TestGoCacheTrimMaxAge(t *testing.T) {
	t.Setenv("VAELOR_GOCACHE_MAX_AGE", "")
	if d := goCacheTrimMaxAge(); d != defaultGoCacheTrimMaxAge {
		t.Fatalf("empty env must yield the default, got %v", d)
	}
	t.Setenv("VAELOR_GOCACHE_MAX_AGE", "12h")
	if d := goCacheTrimMaxAge(); d != 12*time.Hour {
		t.Fatalf("valid env must be honoured, got %v", d)
	}
	for _, bad := range []string{"banana", "0", "-5h"} {
		t.Setenv("VAELOR_GOCACHE_MAX_AGE", bad)
		if d := goCacheTrimMaxAge(); d != defaultGoCacheTrimMaxAge {
			t.Fatalf("%q must fall back to the default, got %v", bad, d)
		}
	}
}

// The loop must return promptly when the server shutdown context cancels —
// a stuck goroutine at exit is a shutdown hang.
func TestStartGoCacheTrimLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		startGoCacheTrimLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("trim loop did not stop on context cancel")
	}
}
