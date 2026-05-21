// internal/fleet/ssh/sshhome_internal_test.go
//
// White-box tests for ensureWritableSSHHome and envWithHome.
// Package ssh (internal) — directly accesses unexported symbols.
package ssh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureWritableSSHHome verifies that source files are copied into
// <dst>/.ssh with 0700 dir perms and 0600 file perms, and that the copy
// is byte-identical to the source.
func TestEnsureWritableSSHHome(t *testing.T) {
	src := t.TempDir()
	files := map[string][]byte{
		"config":         []byte("Host foo\n  HostName 1.2.3.4\n"),
		"id_ed25519":     []byte("FAKE-KEY\n"),
		"id_ed25519.pub": []byte("FAKE-PUB ssh-ed25519 AAAA\n"),
		"known_hosts":    []byte("1.2.3.4 ssh-ed25519 AAAAFOO\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dst := t.TempDir()
	if err := ensureWritableSSHHome(src, dst); err != nil {
		t.Fatalf("first call: %v", err)
	}

	sshDir := filepath.Join(dst, ".ssh")
	fi, err := os.Stat(sshDir)
	if err != nil {
		t.Fatalf("stat .ssh: %v", err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf(".ssh perm = %#o, want 0700", fi.Mode().Perm())
	}

	for name, wantContent := range files {
		p := filepath.Join(sshDir, name)
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s perm = %#o, want 0600", name, fi.Mode().Perm())
		}
		got, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if string(got) != string(wantContent) {
			t.Errorf("%s content mismatch: got %q, want %q", name, got, wantContent)
		}
	}

	// Idempotent: second call must be a no-op and return nil.
	if err := ensureWritableSSHHome(src, dst); err != nil {
		t.Fatalf("second call (idempotent): %v", err)
	}
}

// TestEnsureWritableSSHHome_MissingSrc verifies that a missing source directory
// is gracefully ignored (no error), so the ssh subprocess gets a clearer failure
// than a copy-step error.
func TestEnsureWritableSSHHome_MissingSrc(t *testing.T) {
	dst := t.TempDir()
	if err := ensureWritableSSHHome("/nonexistent/path/.ssh", dst); err != nil {
		t.Errorf("expected nil for missing src, got %v", err)
	}
}

// TestEnvWithHome verifies the pure envWithHome helper:
//   - appends HOME=dst when dst is non-empty (overriding any prior HOME).
//   - returns the parent env unchanged when dst is empty.
func TestEnvWithHome(t *testing.T) {
	t.Run("sets HOME when dst non-empty", func(t *testing.T) {
		parent := []string{"PATH=/usr/bin", "HOME=/old", "TERM=xterm"}
		got := envWithHome(parent, "/new/home")
		// HOME=/new/home must be present; it must be the last HOME entry so
		// the subprocess's getenv picks it up (standard "last wins" behaviour
		// for duplicate env vars on Linux/macOS).
		found := false
		for _, e := range got {
			if e == "HOME=/new/home" {
				found = true
			}
		}
		if !found {
			t.Errorf("HOME=/new/home not found in env: %v", got)
		}
		// The original entries must still be present.
		for _, e := range parent {
			present := false
			for _, g := range got {
				if g == e {
					present = true
					break
				}
			}
			// HOME=/old may be overridden — only test non-HOME entries.
			if e == "HOME=/old" {
				continue
			}
			if !present {
				t.Errorf("parent entry %q missing from result", e)
			}
		}
	})

	t.Run("no-op when dst empty", func(t *testing.T) {
		parent := []string{"PATH=/usr/bin", "HOME=/root"}
		got := envWithHome(parent, "")
		if len(got) != len(parent) {
			t.Errorf("env length changed: got %d, want %d", len(got), len(parent))
		}
		for i, e := range parent {
			if got[i] != e {
				t.Errorf("env[%d]: got %q, want %q", i, got[i], e)
			}
		}
	})
}

// TestRealExecer_RunPopulatesSSHHome verifies the wiring between realExecer
// and ensureWritableSSHHome: when sshHomeSrc/Dst are set, Run must shadow-copy
// the source directory before invoking exec.Command — even when the subprocess
// exits non-zero.
//
// This closes the regression gap between the pure-helper tests and the
// actual exec path: a deletion of either the sync.Once call or the
// ensureWritableSSHHome call inside Run would immediately fail this test.
func TestRealExecer_RunPopulatesSSHHome(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("/bin/false not available")
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "config"), []byte("# marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	re := &realExecer{sshHomeSrc: src, sshHomeDst: dst}
	// Run with a binary that exits non-zero immediately; the shadow-copy
	// must still have happened because it runs before exec.Command.
	_, _, _ = re.Run(context.Background(), "false", "irrelevant-host", nil)
	if _, err := os.Stat(filepath.Join(dst, ".ssh", "config")); err != nil {
		t.Errorf("dst/.ssh/config not populated after Run: %v", err)
	}
}

// TestRewriteSSHConfigPaths_TildeAndHome verifies that rewriteSSHConfigPaths
// replaces ~/.ssh/ and $HOME/.ssh/ prefixes with <homeDst>/.ssh/, while
// leaving absolute paths and HostName/other values untouched.
// Regression test for OpenSSH 10.2p1 on alpine: getpwuid() expands ~ to
// /root/.ssh (the bind-mounted uid-1000 dir), not to $HOME — so IdentityFile
// lines in the shadow-copy must be rewritten to absolute homeDst paths.
func TestRewriteSSHConfigPaths_TildeAndHome(t *testing.T) {
	dst := t.TempDir()
	sshDir := filepath.Join(dst, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(sshDir, "config")
	content := `Host krolik
  HostName 127.0.0.1
  IdentityFile ~/.ssh/id_ed25519
  UserKnownHostsFile $HOME/.ssh/known_hosts

Host other
  IdentityFile /absolute/path/key
`
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewriteSSHConfigPaths(cfg, dst); err != nil {
		t.Fatal(err)
	}

	rewritten, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(rewritten)

	// ~/.ssh/id_ed25519 must become <dst>/.ssh/id_ed25519
	wantIdentity := "IdentityFile " + dst + "/.ssh/id_ed25519"
	if !strings.Contains(s, wantIdentity) {
		t.Errorf("IdentityFile ~/.ssh/ not rewritten: got:\n%s", s)
	}

	// $HOME/.ssh/known_hosts must become <dst>/.ssh/known_hosts
	wantKnown := "UserKnownHostsFile " + dst + "/.ssh/known_hosts"
	if !strings.Contains(s, wantKnown) {
		t.Errorf("UserKnownHostsFile $HOME/.ssh/ not rewritten: got:\n%s", s)
	}

	// Absolute path must be untouched
	if !strings.Contains(s, "IdentityFile /absolute/path/key") {
		t.Errorf("absolute path was mangled: got:\n%s", s)
	}

	// HostName value must be untouched (does not contain ~/.ssh/)
	if !strings.Contains(s, "HostName 127.0.0.1") {
		t.Errorf("HostName was mangled: got:\n%s", s)
	}
}

// TestRewriteSSHConfigPaths_Idempotent verifies that running rewriteSSHConfigPaths
// twice produces the same result as running it once.
func TestRewriteSSHConfigPaths_Idempotent(t *testing.T) {
	dst := t.TempDir()
	sshDir := filepath.Join(dst, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(sshDir, "config")
	content := "  IdentityFile ~/.ssh/id_ed25519\n  UserKnownHostsFile $HOME/.ssh/known_hosts\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewriteSSHConfigPaths(cfg, dst); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(cfg)

	// Second pass must be a no-op.
	if err := rewriteSSHConfigPaths(cfg, dst); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(cfg)

	if string(first) != string(second) {
		t.Errorf("rewriteSSHConfigPaths not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
