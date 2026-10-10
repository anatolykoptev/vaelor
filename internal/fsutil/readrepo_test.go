package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const (
	testDeadline = time.Second
	testMax      = 64
)

// within runs fn and fails the test, instead of hanging the package, when it
// does not return within testDeadline.
func within(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(testDeadline):
		fmt.Fprintf(os.Stderr, "FAIL %s: call did not return within %s\n", t.Name(), testDeadline)
		os.Exit(1)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadRepoFile_Regular(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), []byte("hello"))
	got, err := ReadRepoFile(root, "a.txt", testMax)
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadRepoFile_Refusals(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "canary"), []byte("CANARY-TOKEN"))
	writeFile(t, filepath.Join(root, "real.txt"), []byte("inside"))
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink(filepath.Join(outside, "canary"), filepath.Join(root, "out-link")))
	must(os.Symlink("real.txt", filepath.Join(root, "in-link")))
	must(os.Symlink("/dev/zero", filepath.Join(root, "zero-link")))
	must(syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600))
	must(os.Mkdir(filepath.Join(root, "dir"), 0o700))
	must(os.Symlink(outside, filepath.Join(root, "dir-link")))

	cases := []struct {
		name, rel string
		want      error
	}{
		{"symlink outside root", "out-link", ErrNotRegular},
		{"symlink inside root", "in-link", ErrNotRegular},
		{"symlink to endless device", "zero-link", ErrNotRegular},
		{"fifo", "fifo", ErrNotRegular},
		{"directory", "dir", ErrNotRegular},
		{"escape via dotdot", "../" + filepath.Base(outside) + "/canary", nil},
		{"escape via symlinked dir", "dir-link/canary", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			within(t, func() {
				got, err := ReadRepoFile(root, tc.rel, testMax)
				if err == nil {
					t.Fatalf("expected refusal, got %q", got)
				}
				if tc.want != nil && !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if bytes.Contains([]byte(err.Error()), []byte("CANARY")) || len(got) != 0 {
					t.Fatalf("refusal leaked content: %v %q", err, got)
				}
			})
		})
	}
}

func TestReadRepoFile_TooLarge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big"), bytes.Repeat([]byte("x"), testMax+1))
	writeFile(t, filepath.Join(root, "exact"), bytes.Repeat([]byte("x"), testMax))
	if _, err := ReadRepoFile(root, "big", testMax); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: err = %v, want ErrTooLarge", err)
	}
	if got, err := ReadRepoFile(root, "exact", testMax); err != nil || len(got) != testMax {
		t.Fatalf("exact: len=%d err=%v", len(got), err)
	}
}

func TestReadRepoFilePrefix(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big"), bytes.Repeat([]byte("x"), testMax*4))
	got, err := ReadRepoFilePrefix(root, "big", testMax)
	if err != nil || len(got) != testMax {
		t.Fatalf("len=%d err=%v, want exactly %d bytes", len(got), err, testMax)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(root, "z")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRepoFilePrefix(root, "z", testMax); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("prefix must still refuse non-regular: %v", err)
	}
}

func TestReadRepoFile_Missing(t *testing.T) {
	if _, err := ReadRepoFile(t.TempDir(), "nope", testMax); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}

// TestReadRepoFile_AllocationIsBounded proves the read itself is bounded by max
// (not merely rejected after the fact): a sparse 256 MiB file must not cause a
// 256 MiB allocation.
func TestReadRepoFile_AllocationIsBounded(t *testing.T) {
	const (
		hugeSize   = 256 << 20
		allocLimit = 16 << 20
	)
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "huge"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(hugeSize); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _ = ReadRepoFile(root, "huge", testMax)
	_, _ = ReadRepoFilePrefix(root, "huge", testMax)
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > allocLimit {
		t.Fatalf("read of oversized file allocated %d bytes, want <= %d", got, allocLimit)
	}
}
