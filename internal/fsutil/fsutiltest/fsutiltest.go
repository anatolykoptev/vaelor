// Package fsutiltest holds shared fixtures for tests that check a reader
// refuses symlinked and special files chosen by an untrusted checkout.
package fsutiltest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	// Token is a unique string placed in canary files outside the repo under
	// test. It must appear in no output of the code under test.
	Token = "CANARY-7f3a91c2"

	// Deadline is how long a call may take before the test fails. An unbounded
	// read of an endless device would otherwise OOM or hang the whole package.
	Deadline = 3 * time.Second

	// EndlessDevice is a device that yields bytes forever.
	EndlessDevice = "/dev/zero"
)

// Within runs fn and fails the test, rather than hanging the package, when it
// does not return within Deadline.
func Within(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(Deadline):
		t.Fatalf("call did not return within %s (unbounded read of a special file?)", Deadline)
	}
}

// WriteOutside writes content to a fresh file outside any repo under test and
// returns its path.
func WriteOutside(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "outside-canary")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Symlink creates root/rel as a symlink to target, creating parent dirs.
func Symlink(t *testing.T, root, rel, target string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

// WriteFile writes a regular file root/rel, creating parent dirs.
func WriteFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
