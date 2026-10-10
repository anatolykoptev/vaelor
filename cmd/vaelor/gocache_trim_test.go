package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	fakeCacheOldAge   = 96 * time.Hour
	fakeCacheFreshAge = time.Hour
	fakeKeyLen        = 64
)

// entryName builds a real-shaped cache entry name: 64 hex chars + suffix.
func entryName(c byte, suffix string) string {
	return strings.Repeat(string(c), fakeKeyLen) + suffix
}

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

// writeGoCacheReadme writes the README the go tool leaves at a cache root.
func writeGoCacheReadme(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "README")
	body := goCacheReadmePrefix + "\nSee golang.org to learn more about Go.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	old := time.Now().Add(-fakeCacheOldAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes README: %v", err)
	}
}

// Deletable entries in fakeGoCache: ab/<a>-a (100B) and 0f/<c>-d (200B).
const (
	fakeDeletedFiles = 2
	fakeDeletedBytes = 300
)

// fakeGoCache builds a GOCACHE-shaped temp dir: README + trim.txt at the root
// (old but never eligible), two-hex entry subdirs mixing stale and fresh
// real-shaped entries, an old non-entry file in a hex dir, a locale-style hex
// dir (de/), non-hex dirs, a "-d" directory and a nested dir inside a hex
// subdir, and a symlink whose target lives outside the cache.
func fakeGoCache(t *testing.T) (dir, outsideTarget string) {
	t.Helper()
	dir = t.TempDir()
	writeGoCacheReadme(t, dir)
	writeCacheEntry(t, filepath.Join(dir, "trim.txt"), 16, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", entryName('a', "-a")), 100, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", entryName('b', "-d")), 50, fakeCacheFreshAge)
	writeCacheEntry(t, filepath.Join(dir, "0f", entryName('c', "-d")), 200, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "0f", "ff00ff00-s"), 40, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "de", "messages.po"), 40, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", entryName('e', "-d"), "main.bin"), 70, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "zz", "not-hex"), 30, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "abc", "three-chars"), 30, fakeCacheOldAge)
	writeCacheEntry(t, filepath.Join(dir, "ab", "nested", "deep-d"), 30, fakeCacheOldAge)
	outside := t.TempDir()
	outsideTarget = filepath.Join(outside, "victim")
	writeCacheEntry(t, outsideTarget, 30, fakeCacheOldAge)
	if err := os.Symlink(outsideTarget, filepath.Join(dir, "ab", entryName('f', "-a"))); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return dir, outsideTarget
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to survive the trim: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted, stat err=%v", path, err)
	}
}

// Stale entries inside two-hex subdirs are deleted; fresh entries survive;
// the returned counts and bytes are exact.
//
// Mutation that must turn it RED: invert the age comparison in
// trimGoCacheSubdir (keep-stale/delete-fresh) — the fresh entry is then gone
// and the stale ones remain.
func TestTrimGoCacheDir_EvictsOnlyStaleEntries(t *testing.T) {
	dir, _ := fakeGoCache(t)

	st := trimGoCacheDir(context.Background(), dir, 48*time.Hour, time.Now())

	if st.errs != 0 || st.firstErr != nil {
		t.Fatalf("expected a clean run, got errs=%d firstErr=%v", st.errs, st.firstErr)
	}
	if st.files != fakeDeletedFiles || st.bytes != fakeDeletedBytes {
		t.Fatalf("expected %d files / %d bytes removed, got %d files / %d bytes",
			fakeDeletedFiles, fakeDeletedBytes, st.files, st.bytes)
	}
	assertMissing(t, filepath.Join(dir, "ab", entryName('a', "-a")))
	assertMissing(t, filepath.Join(dir, "0f", entryName('c', "-d")))
	assertExists(t, filepath.Join(dir, "ab", entryName('b', "-d")))
}

// Anything that is not a cache entry survives regardless of age: root files,
// non-entry names in hex dirs (ff00ff00-s, de/messages.po), non-hex dirs,
// "-d" directories, nested dirs and symlinks (never followed).
//
// Mutations that must turn it RED: drop the goCacheEntryPattern check in
// trimGoCacheSubdir (de/messages.po, 0f/ff00ff00-s and the "-d" directory's
// handling are then exposed), or drop the hex-subdir check in
// trimGoCacheDir (zz/not-hex and abc/three-chars are then trimmed).
func TestTrimGoCacheDir_NeverTouchesNonEntries(t *testing.T) {
	dir, outsideTarget := fakeGoCache(t)

	_ = trimGoCacheDir(context.Background(), dir, 48*time.Hour, time.Now())

	for _, p := range []string{
		filepath.Join(dir, "README"),
		filepath.Join(dir, "trim.txt"),
		filepath.Join(dir, "0f", "ff00ff00-s"),
		filepath.Join(dir, "de", "messages.po"),
		filepath.Join(dir, "zz", "not-hex"),
		filepath.Join(dir, "abc", "three-chars"),
		filepath.Join(dir, "ab", "nested", "deep-d"),
		filepath.Join(dir, "ab", entryName('e', "-d"), "main.bin"),
		filepath.Join(dir, "ab", entryName('f', "-a")),
		outsideTarget,
	} {
		assertExists(t, p)
	}
}

// A dir without the Go cache README is skipped whole, even when it contains
// old files named exactly like cache entries.
//
// Mutation that must turn it RED: drop the README check in
// validateGoCacheRoot.
func TestTrimGoCacheDir_NoReadme_DirSkipped(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "ab", entryName('a', "-a"))
	writeCacheEntry(t, victim, 100, fakeCacheOldAge)

	st := trimGoCacheDir(context.Background(), dir, 48*time.Hour, time.Now())

	if st.files != 0 || st.bytes != 0 {
		t.Fatalf("dir without README must be skipped, got %+v", st)
	}
	assertExists(t, victim)
}

// A README that is not Go's is not enough either.
func TestValidateGoCacheRoot_ForeignReadme_Refused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("my project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateGoCacheRoot(dir); err == nil {
		t.Fatal("foreign README must be refused")
	}
}

// Relative paths, "/" and the home dir are refused outright.
func TestValidateGoCacheRoot_RefusesUnsafeRoots(t *testing.T) {
	home := t.TempDir()
	writeGoCacheReadme(t, home) // a valid-looking cache, but it is $HOME
	t.Setenv("HOME", home)

	for name, dir := range map[string]string{
		"relative": "some/relative/cache",
		"dot":      ".",
		"root":     "/",
		"home":     home,
	} {
		if err := validateGoCacheRoot(dir); err == nil {
			t.Errorf("%s (%q) must be refused", name, dir)
		}
	}
	st := trimGoCacheDir(context.Background(), "some/relative/cache", 48*time.Hour, time.Now())
	if st.files != 0 || st.errs != 0 {
		t.Fatalf("refused dir must be a silent no-op for counters, got %+v", st)
	}
}

// A valid cache passes validation.
func TestValidateGoCacheRoot_ValidCache_Accepted(t *testing.T) {
	dir, _ := fakeGoCache(t)
	if err := validateGoCacheRoot(dir); err != nil {
		t.Fatalf("real-shaped cache must be accepted: %v", err)
	}
}

// A cancelled context stops the pass before any subdir is touched.
func TestTrimGoCacheDir_CancelledContext_StopsEarly(t *testing.T) {
	dir, _ := fakeGoCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	st := trimGoCacheDir(ctx, dir, 48*time.Hour, time.Now())

	if st.files != 0 {
		t.Fatalf("cancelled ctx must delete nothing, got %+v", st)
	}
	assertExists(t, filepath.Join(dir, "ab", entryName('a', "-a")))
}

// An empty cache dir (README only) is a no-op, and a missing dir never
// reaches the trim — it is dropped at resolution.
func TestTrimGoCacheDir_EmptyAndMissing_AreNoOps(t *testing.T) {
	dir := t.TempDir()
	writeGoCacheReadme(t, dir)
	st := trimGoCacheDir(context.Background(), dir, 48*time.Hour, time.Now())
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

	trimGoCacheDirs(context.Background(), []string{dir}, 48*time.Hour)

	if d := testutil.ToFloat64(goCacheTrimFilesTotal) - beforeF; d != fakeDeletedFiles {
		t.Fatalf("vaelor_gocache_trim_files_total delta = %v, want %d", d, fakeDeletedFiles)
	}
	if d := testutil.ToFloat64(goCacheTrimBytesTotal) - beforeB; d != fakeDeletedBytes {
		t.Fatalf("vaelor_gocache_trim_bytes_total delta = %v, want %d", d, fakeDeletedBytes)
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
