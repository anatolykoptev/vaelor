package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/workspace"
)

// ---------------------------------------------------------------------------
// RewritePath
// ---------------------------------------------------------------------------

func TestRewritePath_NoMappings_ReturnsUnchanged(t *testing.T) {
	t.Parallel()
	got := workspace.RewritePath("/host/src/foo", nil)
	if got != "/host/src/foo" {
		t.Fatalf("want %q, got %q", "/host/src/foo", got)
	}
}

func TestRewritePath_MatchingPrefix_Rewrites(t *testing.T) {
	t.Parallel()
	mappings := []analyze.PathMapping{{External: "/host/src", Internal: "/container/src"}}
	got := workspace.RewritePath("/host/src/foo", mappings)
	want := "/container/src/foo"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRewritePath_NoMatchingPrefix_ReturnsUnchanged(t *testing.T) {
	t.Parallel()
	mappings := []analyze.PathMapping{{External: "/other", Internal: "/container"}}
	got := workspace.RewritePath("/host/src/foo", mappings)
	if got != "/host/src/foo" {
		t.Fatalf("want unchanged, got %q", got)
	}
}

func TestRewritePath_FirstMatchWins(t *testing.T) {
	t.Parallel()
	mappings := []analyze.PathMapping{
		{External: "/host", Internal: "/first"},
		{External: "/host/src", Internal: "/second"},
	}
	got := workspace.RewritePath("/host/src/foo", mappings)
	// First mapping matches /host prefix.
	want := "/first/src/foo"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// TranslateDirs
// ---------------------------------------------------------------------------

func TestTranslateDirs_EmptyMappings_ReturnsInput(t *testing.T) {
	t.Parallel()
	dirs := []string{"/a", "/b"}
	got := workspace.TranslateDirs(dirs, nil)
	for i, d := range dirs {
		if got[i] != d {
			t.Fatalf("idx %d: want %q, got %q", i, d, got[i])
		}
	}
}

func TestTranslateDirs_AppliesMappings(t *testing.T) {
	t.Parallel()
	mappings := []analyze.PathMapping{{External: "/host/repos", Internal: "/repos"}}
	dirs := []string{"/host/repos/foo", "/host/repos/bar", "/other/baz"}
	got := workspace.TranslateDirs(dirs, mappings)
	want := []string{"/repos/foo", "/repos/bar", "/other/baz"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idx %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestTranslateDirs_ReturnsNewSlice(t *testing.T) {
	t.Parallel()
	dirs := []string{"/a"}
	mappings := []analyze.PathMapping{{External: "/a", Internal: "/b"}}
	got := workspace.TranslateDirs(dirs, mappings)
	if &got[0] == &dirs[0] {
		t.Fatal("TranslateDirs must return a new slice, not mutate input")
	}
}

// ---------------------------------------------------------------------------
// LocalSource.Root
// ---------------------------------------------------------------------------

func TestLocalSource_Root_ExistingDir_NoMappings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := workspace.LocalSource{Path: dir}
	got, cleanup, err := s.Root(context.Background())
	defer cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("want %q, got %q", dir, got)
	}
}

func TestLocalSource_Root_AppliesMapping(t *testing.T) {
	t.Parallel()
	// Create a real dir that acts as the "internal" path.
	dir := t.TempDir()
	// external prefix is "/fake/host", internal prefix is dir.
	const external = "/fake/host"
	mappings := []analyze.PathMapping{{External: external, Internal: dir}}
	s := workspace.LocalSource{
		Path:     external + "/sub",
		Mappings: mappings,
	}
	// Create the sub-directory so Stat passes.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, cleanup, err := s.Root(context.Background())
	defer cleanup()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sub {
		t.Fatalf("want %q, got %q", sub, got)
	}
}

func TestLocalSource_Root_NonExistent_ReturnsError(t *testing.T) {
	t.Parallel()
	s := workspace.LocalSource{Path: "/does/not/exist/ever"}
	_, cleanup, err := s.Root(context.Background())
	defer cleanup()
	if err == nil {
		t.Fatal("want error for non-existent path, got nil")
	}
}

func TestLocalSource_Root_FileNotDir_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := workspace.LocalSource{Path: f}
	_, cleanup, err := s.Root(context.Background())
	defer cleanup()
	if err == nil {
		t.Fatal("want error for file path, got nil")
	}
}

// ---------------------------------------------------------------------------
// TranslateDirs integrated with discoverRepos pattern
// ---------------------------------------------------------------------------

// TestTranslateDirs_ZeroLengthInput returns empty without panic.
func TestTranslateDirs_ZeroLengthInput(t *testing.T) {
	t.Parallel()
	got := workspace.TranslateDirs(nil, []analyze.PathMapping{{External: "/a", Internal: "/b"}})
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// TranslateDirs — copy invariants
// ---------------------------------------------------------------------------

// TestTranslateDirs_EmptyMappingsNonNil_ReturnsCopy ensures that a non-nil
// but zero-length mappings slice still causes TranslateDirs to return a new
// slice, not the original. This covers the gap flagged in code review: the
// nil-mappings fast path previously returned dirs unchanged (same pointer).
func TestTranslateDirs_EmptyMappingsNonNil_ReturnsCopy(t *testing.T) {
	t.Parallel()
	dirs := []string{"/a", "/b"}
	got := workspace.TranslateDirs(dirs, []analyze.PathMapping{})
	if len(got) != len(dirs) {
		t.Fatalf("want len %d, got %d", len(dirs), len(got))
	}
	for i := range dirs {
		if got[i] != dirs[i] {
			t.Errorf("idx %d: want %q, got %q", i, dirs[i], got[i])
		}
	}
	// Mutation of returned slice must NOT affect the original.
	got[0] = "/mutated"
	if dirs[0] == "/mutated" {
		t.Fatal("TranslateDirs returned same slice as input (mutation visible)")
	}
}

// ---------------------------------------------------------------------------
// RemoteSource — token precedence
// ---------------------------------------------------------------------------

// TestRemoteSource_BothStaticTokenAndTokenFunc_TokenFuncWins verifies that
// when both StaticToken and TokenFunc are set, TokenFunc takes precedence.
// This guards the documented contract: "TokenFunc returns an authentication
// token for the clone; may be nil." — StaticToken is the fallback.
func TestRemoteSource_BothStaticTokenAndTokenFunc_TokenFuncWins(t *testing.T) {
	t.Parallel()
	funcToken := "func-token"
	called := false
	s := workspace.RemoteSource{
		Slug:        "owner/repo",
		RepoInput:   "github.com/owner/repo",
		DestDir:     t.TempDir(),
		StaticToken: "static-token",
		TokenFunc: func(ctx context.Context) (string, error) {
			called = true
			return funcToken, nil
		},
	}
	// We can't call s.Root without network, but we can verify the token
	// selection logic by inspecting what RemoteSource exposes. The contract
	// is tested indirectly: if TokenFunc is non-nil, Root calls it.
	// Use a cancelled context so the clone fails fast after token fetch.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cleanup, _ := s.Root(ctx)
	cleanup()
	if !called {
		t.Fatal("expected TokenFunc to be called when both StaticToken and TokenFunc are set")
	}
}
