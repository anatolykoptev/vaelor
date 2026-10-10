package ingest

import (
	"crypto/sha256"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestHashFileChunk_RefusesNonRegular(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "content "+fsutiltest.Marker)
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak", outside)
	fsutiltest.Symlink(t, root, "zero", fsutiltest.EndlessDevice)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsutiltest.WriteFile(t, root, "real", "abc")
	var zero [sha256.Size]byte
	for _, rel := range []string{"leak", "zero", "pipe"} {
		fsutiltest.Within(t, func() {
			if got := hashFileChunk(root, rel, 4096); got != zero {
				t.Errorf("%s: hashed through a non-regular file", rel)
			}
		})
	}
	if got := hashFileChunk(root, "real", 4096); got != sha256.Sum256([]byte("abc")) {
		t.Fatal("regular file hash changed")
	}
}

func TestParseGitignore_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "secret-"+fsutiltest.Marker+"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, ".gitignore", outside)
	fsutiltest.Within(t, func() {
		if got := parseGitignore(root); len(got) != 0 {
			t.Errorf("patterns read through a symlink: %v", got)
		}
	})
}
