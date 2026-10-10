// Package fsutiltest holds shared fixtures for tests that check a reader
// refuses symlinked and special files chosen by an untrusted checkout.
package fsutiltest

import (
	"fmt"
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
	Deadline = time.Second

	// EndlessDevice is a device that yields bytes forever.
	EndlessDevice = "/dev/zero"
)

// Within runs fn and, when it does not return within Deadline, reports the
// regression on stderr and exits the test binary. A plain t.Fatalf is not
// enough here: the stuck call is usually an unbounded read still allocating in
// its goroutine, and letting the rest of the package run would exhaust memory.
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
		fmt.Fprintf(os.Stderr, "FAIL %s: call did not return within %s (unbounded read of a special file?)\n", t.Name(), Deadline)
		os.Exit(1)
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
