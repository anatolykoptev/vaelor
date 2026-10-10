package envdetect

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/freshness"
	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// TestBuildNPMToolchain_RefusedManifestDegrades: a manifest that the read
// refuses must not abort detection nor leak content; the toolchain keeps its
// convention-only install command.
func TestBuildNPMToolchain_RefusedManifestDegrades(t *testing.T) {
	outside := fsutiltest.WriteOutside(t,
		fmt.Sprintf(`{"scripts":{"build":"echo %s"}}`, fsutiltest.Marker))
	for name, target := range map[string]string{"canary": outside, "device": fsutiltest.EndlessDevice} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fsutiltest.Symlink(t, root, "package.json", target)
			m := freshness.ManifestInfo{ManifestPath: "package.json", Language: "javascript"}
			var tc Toolchain
			var err error
			fsutiltest.Within(t, func() { tc, err = buildNPMToolchain(root, ".", m) })
			if err != nil {
				t.Fatalf("refused manifest must degrade, got error %v", err)
			}
			if len(tc.Commands) != 1 || strings.Contains(fmt.Sprintf("%+v", tc), fsutiltest.Marker) {
				t.Fatalf("expected install-only toolchain without content, got %+v", tc)
			}
		})
	}
}

func TestAccumulatePython_RefusedPyprojectDegrades(t *testing.T) {
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "pyproject.toml", fsutiltest.EndlessDevice)
	acc := &pythonAccum{dir: "."}
	m := freshness.ManifestInfo{ManifestPath: "pyproject.toml"}
	fsutiltest.Within(t, func() { accumulatePython(acc, root, "pyproject.toml", m) })
	if len(acc.scriptNames) != 0 {
		t.Fatalf("scripts parsed from a refused file: %v", acc.scriptNames)
	}
}

// TestParseMakefileTargets_RefusesSymlink: the Makefile is read by name from
// the checkout, so a link must neither supply targets nor block.
func TestParseMakefileTargets_RefusesSymlink(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "build:\n\techo "+fsutiltest.Marker+"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "Makefile", outside)
	fsutiltest.Symlink(t, root, "sub/Makefile", fsutiltest.EndlessDevice)
	for _, rel := range []string{"Makefile", "sub/Makefile"} {
		fsutiltest.Within(t, func() {
			targets, err := parseMakefileTargets(root, rel)
			if err != nil || len(targets) != 0 {
				t.Errorf("%s: targets from a refused file: %v, %v", rel, targets, err)
			}
		})
	}
}
