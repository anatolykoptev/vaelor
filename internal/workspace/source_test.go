package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/workspace"
)

// ---------------------------------------------------------------------------
// RewritePath
// ---------------------------------------------------------------------------

func TestRewritePath_NoMappings_ReturnsUnchanged(t *testing.T) {
	got := workspace.RewritePath("/host/src/foo", nil)
	if got != "/host/src/foo" {
		t.Fatalf("want %q, got %q", "/host/src/foo", got)
	}
}

func TestRewritePath_MatchingPrefix_Rewrites(t *testing.T) {
	mappings := []analyze.PathMapping{{External: "/host/src", Internal: "/container/src"}}
	got := workspace.RewritePath("/host/src/foo", mappings)
	want := "/container/src/foo"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRewritePath_NoMatchingPrefix_ReturnsUnchanged(t *testing.T) {
	mappings := []analyze.PathMapping{{External: "/other", Internal: "/container"}}
	got := workspace.RewritePath("/host/src/foo", mappings)
	if got != "/host/src/foo" {
		t.Fatalf("want unchanged, got %q", got)
	}
}

func TestRewritePath_FirstMatchWins(t *testing.T) {
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
	dirs := []string{"/a", "/b"}
	got := workspace.TranslateDirs(dirs, nil)
	for i, d := range dirs {
		if got[i] != d {
			t.Fatalf("idx %d: want %q, got %q", i, d, got[i])
		}
	}
}

func TestTranslateDirs_AppliesMappings(t *testing.T) {
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
	s := workspace.LocalSource{Path: "/does/not/exist/ever"}
	_, cleanup, err := s.Root(context.Background())
	defer cleanup()
	if err == nil {
		t.Fatal("want error for non-existent path, got nil")
	}
}

func TestLocalSource_Root_FileNotDir_ReturnsError(t *testing.T) {
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
	got := workspace.TranslateDirs(nil, []analyze.PathMapping{{External: "/a", Internal: "/b"}})
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}
