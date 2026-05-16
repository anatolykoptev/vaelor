package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
)

// TestResolveRoot_LocalPath_NoMappings verifies that resolveRoot returns the
// path unchanged when no PATH_MAPPINGS are configured.
func TestResolveRoot_LocalPath_NoMappings(t *testing.T) {
	dir := t.TempDir()
	deps := analyze.Deps{}
	root, cleanup, err := resolveRoot(context.Background(), dir, "", deps)
	defer cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Fatalf("want %q, got %q", dir, root)
	}
}

// TestResolveRoot_LocalPath_WithMappings is the backward-compat guard for
// PATH_MAPPINGS translation on local paths. Verifies that the path written
// in External prefix form is rewritten to the Internal prefix before stat.
//
// This is the class-of-bug fixed by Phase 2b: before the Source interface
// refactor, resolveRoot already applied rewritePath for local paths.
// This test ensures the behavior is preserved exactly.
func TestResolveRoot_LocalPath_WithMappings(t *testing.T) {
	// Set up a real directory (the "container-internal" path).
	internalBase := t.TempDir()
	sub := filepath.Join(internalBase, "myrepo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	const externalBase = "/fake/host/repos"
	deps := analyze.Deps{
		PathMappings: []analyze.PathMapping{
			{External: externalBase, Internal: internalBase},
		},
	}
	// Caller supplies the host-side path.
	hostPath := externalBase + "/myrepo"
	root, cleanup, err := resolveRoot(context.Background(), hostPath, "", deps)
	defer cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != sub {
		t.Fatalf("want %q (translated), got %q", sub, root)
	}
}

// TestResolveRoot_LocalPath_PrefixStripping verifies that "path=" and "local:"
// prefixes are stripped before processing.
func TestResolveRoot_LocalPath_PrefixStripping(t *testing.T) {
	dir := t.TempDir()
	deps := analyze.Deps{}
	for _, prefix := range []string{"path=", "local:"} {
		t.Run(prefix, func(t *testing.T) {
			root, cleanup, err := resolveRoot(context.Background(), prefix+dir, "", deps)
			defer cleanup()
			if err != nil {
				t.Fatalf("prefix %q: unexpected error: %v", prefix, err)
			}
			if root != dir {
				t.Fatalf("prefix %q: want %q, got %q", prefix, dir, root)
			}
		})
	}
}

// TestRewritePath_DelegatesCorrectly verifies that the package-local rewritePath
// wrapper produces the same result as direct prefix substitution.
func TestRewritePath_DelegatesCorrectly(t *testing.T) {
	mappings := []analyze.PathMapping{{External: "/host", Internal: "/container"}}
	cases := []struct{ in, want string }{
		{"/host/foo", "/container/foo"},
		{"/host", "/container"},
		{"/other", "/other"},
		{"", ""},
	}
	for _, c := range cases {
		got := rewritePath(c.in, mappings)
		if got != c.want {
			t.Errorf("rewritePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMakePathRewrite_NilWhenEmpty verifies that nil is returned when mappings
// is empty, preserving the signal to callers that no worktree .git parsing is needed.
func TestMakePathRewrite_NilWhenEmpty(t *testing.T) {
	fn := makePathRewrite(nil)
	if fn != nil {
		t.Fatal("want nil for empty mappings, got non-nil")
	}
}

// TestMakePathRewrite_NonNilWhenMappingsSet verifies that a valid rewriter is
// returned and applies the mapping correctly.
func TestMakePathRewrite_NonNilWhenMappingsSet(t *testing.T) {
	mappings := []analyze.PathMapping{{External: "/a", Internal: "/b"}}
	fn := makePathRewrite(mappings)
	if fn == nil {
		t.Fatal("want non-nil rewriter")
	}
	if got := fn("/a/x"); got != "/b/x" {
		t.Fatalf("want %q, got %q", "/b/x", got)
	}
}
