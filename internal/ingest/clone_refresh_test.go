package ingest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit prepares a local bare-ish git origin: a non-bare repo with a
// single commit. Tests use it via CloneOpts.CloneURL=file:// so no
// network is required.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	mustRun(t, dir, "git", "init", "-q", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "initial")
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

// TestCloneRepo_CacheHitRefreshesToRemoteHEAD asserts the bug we just
// fixed: a second call to CloneRepo against a slug that already has a
// local clone must pick up commits pushed to the remote between the
// two calls. Pre-fix, the cache-hit branch returned the on-disk state
// unconditionally and the second call would silently serve stale code.
func TestCloneRepo_CacheHitRefreshesToRemoteHEAD(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}

	gitInit(t, origin)

	cloneURL := "file://" + origin
	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: cloneURL,
		Ref:      "main",
	}

	// First call: fresh clone.
	res1, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res1.LocalPath, "README.md")); err != nil {
		t.Fatalf("first clone missing README: %v", err)
	}

	// Push a new file to origin between calls.
	if err := os.WriteFile(filepath.Join(origin, "NEW.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "add NEW")

	// Second call: cache-hit path. Must fetch+reset and surface NEW.md.
	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second clone (cache-hit): %v", err)
	}
	if res2.LocalPath != res1.LocalPath {
		t.Fatalf("expected same local path on cache hit, got %q != %q", res2.LocalPath, res1.LocalPath)
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "NEW.md")); err != nil {
		t.Fatalf("cache-hit returned stale clone: NEW.md missing — %v", err)
	}
}

// TestCloneRepo_CacheHitRecoversFromCorruptClone asserts that when the
// on-disk clone is corrupt (e.g. .git wiped mid-cleanup by a concurrent
// call), refreshClone fails and the code falls through to a fresh clone
// rather than returning a broken path.
func TestCloneRepo_CacheHitRecoversFromCorruptClone(t *testing.T) {
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

	res1, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}

	// Simulate a corrupted clone: wipe .git but leave the worktree.
	if err := os.RemoveAll(filepath.Join(res1.LocalPath, ".git")); err != nil {
		t.Fatal(err)
	}

	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second clone after corruption: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, ".git")); err != nil {
		t.Fatalf("expected fresh clone with .git after corruption recovery: %v", err)
	}
}
