package freshness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestDiscoverManifests_RefusesSymlinkedManifests(t *testing.T) {
	outside := fsutiltest.WriteOutside(t,
		fmt.Sprintf(`{"name":"%s","dependencies":{"%s":"1.0.0"}}`, fsutiltest.Marker, fsutiltest.Marker))
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "package.json", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "sub/package.json", outside)
	fsutiltest.Symlink(t, root, "Cargo.toml", fsutiltest.EndlessDevice)

	var got []ManifestInfo
	fsutiltest.Within(t, func() { got = DiscoverManifests(root) })
	if len(got) != 0 || strings.Contains(fmt.Sprintf("%+v", got), fsutiltest.Marker) {
		t.Fatalf("symlinked manifests must be ignored, got %+v", got)
	}
}

// TestParseManifestFile_RefusesSymlink pins the read site itself.
func TestParseManifestFile_RefusesSymlink(t *testing.T) {
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "package.json", fsutiltest.EndlessDevice)
	var info *ManifestInfo
	fsutiltest.Within(t, func() {
		info = parseManifestFile(root+"/package.json", root, ParsePackageJSON)
	})
	if info != nil {
		t.Fatalf("manifest read through a symlink: %+v", info)
	}
}

func TestFindCargoLock_StaysInsideRoot(t *testing.T) {
	parent := t.TempDir()
	fsutiltest.WriteFile(t, parent, "Cargo.lock", "# above the repo\n")
	root := parent + "/repo"
	fsutiltest.WriteFile(t, root, "crates/a/Cargo.toml", "[package]\n")
	if got := findCargoLock(root, root+"/crates/a"); got != "" {
		t.Fatalf("found a lockfile above the repo root: %q", got)
	}
	fsutiltest.WriteFile(t, root, "Cargo.lock", "# in repo\n")
	if got := findCargoLock(root, root+"/crates/a"); got != "Cargo.lock" {
		t.Fatalf("workspace-root lockfile not found: %q", got)
	}
}

func TestFindCargoLock_IgnoresSymlinkedLock(t *testing.T) {
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "Cargo.lock", fsutiltest.EndlessDevice)
	if got := findCargoLock(root, root); got != "" {
		t.Fatalf("symlinked Cargo.lock accepted: %q", got)
	}
}
