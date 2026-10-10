package coupling

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestReadVerifyFile_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "package x // "+fsutiltest.Token+"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "leak.go", outside)
	for _, rel := range []string{"zero.go", "leak.go"} {
		fsutiltest.Within(t, func() {
			if src, lang := readVerifyFile(root, rel); src != nil || lang != "" {
				t.Errorf("%s: read through a symlink: %q %q", rel, src, lang)
			}
		})
	}
}
