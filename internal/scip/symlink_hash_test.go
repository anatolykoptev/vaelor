package scip

import (
	"crypto/sha256"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestHashFileContent_RefusesNonRegular(t *testing.T) {
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
			if got := hashFileContent(root, rel); got != zero {
				t.Errorf("%s: hashed through a non-regular file", rel)
			}
		})
	}
	if got := hashFileContent(root, "real"); got != sha256.Sum256([]byte("abc")) {
		t.Fatal("regular file hash changed")
	}
}
