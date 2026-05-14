package ingest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefreshClone_InjectsTokenViaGitConfig asserts that when TokenFunc is
// supplied, refreshClone passes the token as a GIT_CONFIG http.extraheader
// rather than mutating .git/config.
// We use a local file:// remote so no network is required.
func TestRefreshClone_InjectsTokenViaGitConfig(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/tokendemo",
		DestDir:  workspace,
		CloneURL: "file://" + origin,
		Ref:      "main",
		TokenFunc: func(_ context.Context) (string, error) {
			return "test-token-123", nil
		},
	}

	// Initial clone.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Push a new commit to origin so fetch actually does something.
	if err := os.WriteFile(filepath.Join(origin, "NEW.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "add NEW")

	// Cache-hit path with TokenFunc — must succeed and not modify .git/config.
	gitConfigBefore := readGitConfig(t, res.LocalPath)

	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("cache-hit with TokenFunc: %v", err)
	}

	gitConfigAfter := readGitConfig(t, res2.LocalPath)
	if gitConfigBefore != gitConfigAfter {
		t.Errorf(".git/config was modified by refreshClone (must be immutable):\nbefore: %s\nafter:  %s", gitConfigBefore, gitConfigAfter)
	}
	if strings.Contains(gitConfigAfter, "test-token-123") {
		t.Error("token leaked into .git/config — must not be persisted")
	}

	// NEW.md must be present after the token-authenticated fetch.
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "NEW.md")); err != nil {
		t.Fatalf("cache-hit with TokenFunc did not fetch new commit: %v", err)
	}
}

// TestRefreshClone_TokenFuncError asserts that a tokenFunc returning an error
// causes refreshClone to fail, which triggers the full re-clone fallback path.
func TestRefreshClone_TokenFuncError(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	callCount := 0
	opts := CloneOpts{
		Slug:     "test/errordemo",
		DestDir:  workspace,
		CloneURL: "file://" + origin,
		Ref:      "main",
		TokenFunc: func(_ context.Context) (string, error) {
			callCount++
			if callCount == 1 {
				// First call (during initial clone) — this is not called for
				// initial clone, only for refresh. Return success sentinel.
				return "initial-token", nil
			}
			return "", errors.New("token refresh failed: upstream unavailable")
		},
	}

	// First clone: no refresh path triggered, succeeds regardless.
	_, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Second call: refresh path triggered, tokenFunc errors → falls back to
	// re-clone, which succeeds (file:// remote doesn't need the token).
	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("should recover via re-clone after tokenFunc error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, ".git")); err != nil {
		t.Fatalf(".git missing after re-clone recovery: %v", err)
	}
}

// TestCloneRepo_BackwardsCompat_NoTokenFunc asserts that existing callers
// without TokenFunc continue to work via embedded URL credentials.
func TestCloneRepo_BackwardsCompat_NoTokenFunc(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	// TokenFunc is nil — old-style behaviour.
	opts := CloneOpts{
		Slug:     "test/compat",
		DestDir:  workspace,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	res1, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}

	if err := os.WriteFile(filepath.Join(origin, "V2.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "v2")

	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("cache-hit without TokenFunc: %v", err)
	}
	if res2.LocalPath != res1.LocalPath {
		t.Fatalf("path changed on cache hit")
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "V2.md")); err != nil {
		t.Fatalf("V2.md missing on cache-hit without TokenFunc: %v", err)
	}
}

// readGitConfig returns the raw content of .git/config for assertion.
func readGitConfig(t *testing.T, repoPath string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoPath, ".git", "config"))
	if err != nil {
		t.Fatalf("read .git/config: %v", err)
	}
	return string(b)
}

// mustRunOutput is like mustRun but returns stdout+stderr.
func mustRunOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
	return string(out)
}
