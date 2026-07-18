package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
)

func TestReverseToHost_MapsPath(t *testing.T) {
	mappings := []analyze.PathMapping{
		{External: "/home/user/src/foo", Internal: "/host/src/foo"},
	}
	got := reverseToHost("/host/src/foo/bar.go", mappings)
	want := "/home/user/src/foo/bar.go"
	if got != want {
		t.Errorf("reverseToHost() = %q, want %q", got, want)
	}
}

func TestReverseToHost_NoMapping_ReturnsUnchanged(t *testing.T) {
	mappings := []analyze.PathMapping{
		{External: "/home/user/src/foo", Internal: "/host/src/foo"},
	}
	got := reverseToHost("/other/path/bar.go", mappings)
	want := "/other/path/bar.go"
	if got != want {
		t.Errorf("reverseToHost() = %q, want %q", got, want)
	}
}

func TestReverseToHost_RelativePath_ReturnsUnchanged(t *testing.T) {
	mappings := []analyze.PathMapping{
		{External: "/home/user/src/foo", Internal: "/host/src/foo"},
	}
	got := reverseToHost("relative/path/bar.go", mappings)
	want := "relative/path/bar.go"
	if got != want {
		t.Errorf("reverseToHost() = %q, want %q", got, want)
	}
}

func TestReverseToHost_MultipleMappings_FirstMatch(t *testing.T) {
	mappings := []analyze.PathMapping{
		{External: "/home/user/src/foo", Internal: "/host/src/foo"},
		{External: "/home/user/src", Internal: "/host/src"},
	}
	// Should match the first mapping (more specific) — but the impl iterates in order.
	// The /host/src/foo prefix matches the first mapping.
	got := reverseToHost("/host/src/foo/pkg/file.go", mappings)
	want := "/home/user/src/foo/pkg/file.go"
	if got != want {
		t.Errorf("reverseToHost() = %q, want %q", got, want)
	}
}

func TestReverseToHost_EmptyMappings_ReturnsUnchanged(t *testing.T) {
	got := reverseToHost("/host/src/foo/bar.go", nil)
	want := "/host/src/foo/bar.go"
	if got != want {
		t.Errorf("reverseToHost() = %q, want %q", got, want)
	}
}

func TestIndexedPathsHint_ContainsKnownDirs(t *testing.T) {
	hint := indexedPathsHint()
	for _, want := range []string{".claude", "vendor", "testdata"} {
		if !strings.Contains(hint, want) {
			t.Errorf("indexedPathsHint() does not contain %q; got:\n%s", want, hint)
		}
	}
}

func TestIndexedPathsHint_ContainsExcludedWord(t *testing.T) {
	hint := indexedPathsHint()
	if !strings.Contains(hint, "excluded") {
		t.Errorf("indexedPathsHint() does not contain 'excluded'; got:\n%s", hint)
	}
}
