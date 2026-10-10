package codegraph

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestReadCodeSignature_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "secret "+fsutiltest.Marker+"\nline2\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "func f() {\n}\n")
	for _, rel := range []string{"leak.go", "zero.go"} {
		fsutiltest.Within(t, func() {
			if got := readCodeSignature(root, rel, 1, 3); got != "" || strings.Contains(got, fsutiltest.Marker) {
				t.Errorf("%s: signature from a symlink: %q", rel, got)
			}
		})
	}
	if got := readCodeSignature(root, "real.go", 1, 3); !strings.Contains(got, "func f()") {
		t.Fatalf("regular file: %q", got)
	}
}
