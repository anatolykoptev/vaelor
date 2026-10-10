package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// A symlink whose target path is longer than the byte gate in
// findOversizedFiles: lstat reports the link length, which passes the gate, and
// the read would then follow the link.
func TestFindOversizedFiles_RefusesSymlinkedSource(t *testing.T) {
	const maxLines = 20 // byte gate = 40
	outside := fsutiltest.WriteOutside(t, strings.Repeat("x := 1 // "+fsutiltest.Marker+"\n", maxLines*3))
	if len(outside) < maxLines*2 {
		t.Skipf("temp path %q too short to pass the byte gate", outside)
	}
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "link.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", strings.Repeat("x := 1\n", maxLines*3))

	var got []string
	fsutiltest.Within(t, func() { got = findOversizedFiles(root, "go", maxLines) })
	if len(got) != 1 || got[0] != "real.go" {
		t.Fatalf("only the regular oversized file may be reported, got %v", got)
	}
}

func TestReadRepoParseSource_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "package x // "+fsutiltest.Marker+"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "package x\n")
	for _, rel := range []string{"leak.go", "zero.go", "../escape.go"} {
		fsutiltest.Within(t, func() {
			if src, err := readRepoParseSource(root, rel, 1<<20); err == nil {
				t.Errorf("%s: read through a symlink or escape: %q", rel, src)
			}
		})
	}
	if src, err := readRepoParseSource(root, "real.go", 1<<20); err != nil || string(src) != "package x\n" {
		t.Fatalf("regular file: %q, %v", src, err)
	}
}

func TestPolicySourceReader_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "secret "+fsutiltest.Marker)
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "package x\n")
	read := policySourceReader(root)
	for _, rel := range []string{"leak.go", "zero.go"} {
		fsutiltest.Within(t, func() {
			if got := read(rel); got != "" {
				t.Errorf("%s: policy callback returned %q", rel, got)
			}
		})
	}
	if got := read("real.go"); got != "package x\n" {
		t.Fatalf("regular file: %q", got)
	}
}

func TestSymbolLineCount_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "func f() {\n\treturn\n}\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "func f() {\n\treturn\n}\n")
	for _, rel := range []string{"leak.go", "zero.go"} {
		fsutiltest.Within(t, func() {
			if n := symbolLineCount(root, rel, 1); n != 0 {
				t.Errorf("%s: counted %d lines through a symlink", rel, n)
			}
		})
	}
	if n := symbolLineCount(root, "real.go", 1); n != 3 {
		t.Fatalf("regular file: %d lines, want 3", n)
	}
}
