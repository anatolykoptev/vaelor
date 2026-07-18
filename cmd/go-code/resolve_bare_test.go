package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
)

// initGitRepo creates a minimal git checkout at dir so that the bare-name
// resolver's directory probe succeeds. Returns the absolute path.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

// TestResolveRoot_BareName_ResolvesAgainstLocalRepoDirs is the RED test for the
// bare-repo-name resolution bug: a caller-supplied bare basename like
// "acme-web" (no slash, no scheme — not a forge slug) must resolve to the
// matching checkout under deps.LocalRepoDirs (e.g. /host/src), NOT fail with a
// CWD-relative "stat acme-web: no such file or directory".
//
// This is the exact symptom the operator hit: every subagent prompt + the
// go-code cheatsheet calls go-code with bare repo names; before this fix they
// all 404'd the resolver and silently degraded to Grep/Read.
func TestResolveRoot_BareName_ResolvesAgainstLocalRepoDirs(t *testing.T) {
	srcRoot := t.TempDir() // stands in for /host/src
	repoDir := filepath.Join(srcRoot, "acme-web")
	initGitRepo(t, repoDir)

	deps := analyze.Deps{LocalRepoDirs: []string{srcRoot}}

	root, cleanup, err := resolveRoot(context.Background(), "acme-web", "", deps)
	defer cleanup()
	if err != nil {
		t.Fatalf("bare name should resolve against LocalRepoDirs, got error: %v", err)
	}
	if root != repoDir {
		t.Fatalf("want %q, got %q", repoDir, root)
	}
}

// TestResolveRoot_BareName_UnknownReturnsClearError verifies that a bare name
// with no matching checkout returns an error (so callers fail loudly) and does
// NOT silently succeed. The error path is the original LocalSource stat — the
// fallback is best-effort, not a replacement.
func TestResolveRoot_BareName_UnknownReturnsClearError(t *testing.T) {
	srcRoot := t.TempDir() // empty — no checkout matches
	deps := analyze.Deps{LocalRepoDirs: []string{srcRoot}}

	_, cleanup, err := resolveRoot(context.Background(), "this-repo-does-not-exist-xyz", "", deps)
	defer cleanup()
	if err == nil {
		t.Fatal("unknown bare name must return an error, got nil")
	}
}

// TestResolveRoot_BareName_NoLocalRepoDirs verifies that with no LocalRepoDirs
// configured the bare name falls through to the original LocalSource behavior
// (CWD-relative stat), preserving the pre-fix error for callers without an
// indexed-repo registry. Guards against the fallback changing the no-registry path.
func TestResolveRoot_BareName_NoLocalRepoDirs(t *testing.T) {
	deps := analyze.Deps{} // no LocalRepoDirs
	_, cleanup, err := resolveRoot(context.Background(), "acme-web", "", deps)
	defer cleanup()
	if err == nil {
		t.Fatal("bare name with no LocalRepoDirs must error (CWD-relative stat), got nil")
	}
}

// TestResolveRoot_AbsolutePath_StillWorks is the backward-compat guard: the
// working absolute-path case must be unaffected by the bare-name fallback.
func TestResolveRoot_AbsolutePath_StillWorks(t *testing.T) {
	dir := t.TempDir()
	deps := analyze.Deps{LocalRepoDirs: []string{t.TempDir()}}
	root, cleanup, err := resolveRoot(context.Background(), dir, "", deps)
	defer cleanup()
	if err != nil {
		t.Fatalf("absolute path must still resolve, got error: %v", err)
	}
	if root != dir {
		t.Fatalf("want %q, got %q", dir, root)
	}
}

// TestBareNameCheckoutFor covers the registry-match helper directly, including
// the path-traversal escape guard (a name containing a separator or ".." must
// not be allowed to resolve outside the configured registry dirs).
func TestBareNameCheckoutFor(t *testing.T) {
	srcRoot := t.TempDir()
	repoDir := filepath.Join(srcRoot, "acme-web")
	initGitRepo(t, repoDir)
	// A plain directory (no .git) must NOT match — only real checkouts.
	if err := os.MkdirAll(filepath.Join(srcRoot, "not-a-checkout"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := []string{srcRoot}

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"match", "acme-web", repoDir},
		{"no-git-dir", "not-a-checkout", ""},
		{"missing", "nope", ""},
		{"empty", "", ""},
		{"slash-escape", "../etc", ""},
		{"backslash-escape", `..\etc`, ""},
		{"dot", ".", ""},
		{"dotdot", "..", ""},
		{"nested-slash", "sub/acme-web", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bareNameCheckoutFor(c.input, dirs); got != c.want {
				t.Fatalf("bareNameCheckoutFor(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}

	// No dirs configured → never matches.
	if got := bareNameCheckoutFor("acme-web", nil); got != "" {
		t.Fatalf("no dirs: want \"\", got %q", got)
	}
}
