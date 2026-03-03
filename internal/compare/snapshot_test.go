package compare_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/compare"
)

// findRepoRoot walks upward from the current working directory until it finds
// a directory containing go.mod, which is the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found — cannot locate repo root")
		}
		dir = parent
	}
}

func TestBuildSnapshot(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if snap.Name != filepath.Base(root) {
		t.Errorf("Name = %q, want %q", snap.Name, filepath.Base(root))
	}

	if snap.FileCount == 0 {
		t.Error("FileCount = 0, want > 0")
	}

	if len(snap.Symbols) == 0 {
		t.Error("Symbols is empty, want > 0")
	}

	if snap.TotalLines == 0 {
		t.Error("TotalLines = 0, want > 0")
	}

	if snap.Language == "" {
		t.Error("Language is empty")
	}
}

func TestBuildSnapshotWithFocus(t *testing.T) {
	root := findRepoRoot(t)
	focus := "internal/parser"

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Focus: focus,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot with focus: %v", err)
	}

	if snap.FileCount == 0 {
		t.Errorf("FileCount = 0 with focus %q, want > 0", focus)
	}

	for _, f := range snap.Files {
		if !strings.HasPrefix(f.RelPath, focus) {
			t.Errorf("file %q is outside focus %q", f.RelPath, focus)
		}
	}

	if len(snap.Files) == 0 {
		t.Errorf("Files is empty with focus %q", focus)
	}
}

func TestBuildSnapshot_ContentFallback(t *testing.T) {
	root := findRepoRoot(t)

	// "CompareRepos parser" won't match any file path, but should match
	// symbol names like CompareRepos, ParseFile, etc. via content fallback.
	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Focus: "CompareRepos parser",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot content fallback: %v", err)
	}

	if snap.FileCount == 0 {
		t.Error("content fallback: FileCount = 0, expected files matching symbol names")
	}

	if snap.FocusMode != "content" {
		t.Errorf("FocusMode = %q, want %q", snap.FocusMode, "content")
	}
}

func TestBuildSnapshot_PathFocusNoFallback(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Focus: "internal/parser",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot path focus: %v", err)
	}

	if snap.FileCount == 0 {
		t.Error("path focus should match files under internal/parser")
	}

	if snap.FocusMode != "" {
		t.Errorf("FocusMode = %q, want empty (path focus should not trigger fallback)", snap.FocusMode)
	}
}

func TestBuildSnapshot_BodyHash(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Language: "go",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	hashSeen := false
	for _, sym := range snap.Symbols {
		if sym.Body != "" && sym.BodyHash == 0 {
			t.Errorf("symbol %q has body but BodyHash=0", sym.Name)
		}
		if sym.BodyHash != 0 {
			hashSeen = true
		}
	}
	if !hashSeen {
		t.Error("no symbols with BodyHash set — expected at least one")
	}
}
