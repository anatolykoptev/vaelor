package semhealth

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestReadBuildConstraint_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "//go:build linux\n\npackage x\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "//go:build linux\n\npackage x\n")
	for _, rel := range []string{"leak.go", "zero.go"} {
		fsutiltest.Within(t, func() {
			if e := readBuildConstraint(root, rel); e != nil {
				t.Errorf("%s: constraint read through a symlink: %v", rel, e)
			}
		})
	}
	if readBuildConstraint(root, "real.go") == nil {
		t.Fatal("regular file constraint not read")
	}
}
