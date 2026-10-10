package envdetect_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/envdetect"
	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// TestDetect_RefusesSymlinkedFiles is the call-site gate: a checkout whose
// manifests and Makefile are links to an endless device or an outside file
// yields no toolchain, promptly, with none of the outside content.
func TestDetect_RefusesSymlinkedFiles(t *testing.T) {
	outside := fsutiltest.WriteOutside(t,
		fmt.Sprintf(`{"scripts":{"build":"%s"}}`, fsutiltest.Marker))
	makefileOutside := fsutiltest.WriteOutside(t, "build:\n\techo "+fsutiltest.Marker+"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "package.json", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "web/package.json", outside)
	fsutiltest.Symlink(t, root, "pyproject.toml", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "Makefile", makefileOutside)

	var env *envdetect.Environment
	var err error
	fsutiltest.Within(t, func() { env, err = envdetect.Detect(context.Background(), root) })
	if err != nil {
		t.Fatalf("Detect must degrade, not fail: %v", err)
	}
	if len(env.Toolchains) != 0 || strings.Contains(fmt.Sprintf("%+v", env), fsutiltest.Marker) {
		t.Fatalf("symlinked files must yield nothing, got %+v", env)
	}
}
