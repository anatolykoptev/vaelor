package scip

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCopyForIndexing_SymlinkContainment(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"token":"outside"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "main.rs"), "fn main() {}\n")
	mustWrite(t, filepath.Join(repo, "real.json"), `{"ok":true}`)
	// Escaping link with a copyable extension: the exploit shape.
	mustSymlink(t, secret, filepath.Join(repo, "leak.json"))
	// Escaping link to a directory.
	mustSymlink(t, outside, filepath.Join(repo, "leakdir.json"))
	// Benign in-root link: must still be copied (no regression).
	mustSymlink(t, filepath.Join(repo, "real.json"), filepath.Join(repo, "alias.json"))

	before := testutil.ToFloat64(scipSkippedTotal.WithLabelValues("-", SkipReasonSymlinkEscape))

	dst := t.TempDir()
	if err := copyForIndexing(repo, dst); err != nil {
		t.Fatalf("copyForIndexing: %v", err)
	}

	for _, name := range []string{"leak.json", "leakdir.json"} {
		if _, err := os.Lstat(filepath.Join(dst, name)); err == nil {
			t.Errorf("%s escaped the repo root and was copied into the index dir", name)
		}
	}
	// Positive controls: the walk ran and benign content still copies.
	for _, name := range []string{"main.rs", "real.json", "alias.json"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s should have been copied: %v", name, err)
		}
	}
	if got := testutil.ToFloat64(scipSkippedTotal.WithLabelValues("-", SkipReasonSymlinkEscape)) - before; got != 2 {
		t.Errorf("symlink_outside_root counter delta = %v, want 2", got)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
