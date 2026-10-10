package importresolve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// TestBuildConfig_RefusesSymlinkedFiles: files the checkout points at an endless
// device or at a file outside the repo must be ignored — promptly, and without
// any of their content reaching the Config.
func TestBuildConfig_RefusesSymlinkedFiles(t *testing.T) {
	outsideJSON := fsutiltest.WriteOutside(t,
		fmt.Sprintf(`{"name":"%s","exports":{".":"./%s"}}`, fsutiltest.Marker, fsutiltest.Marker))
	outsideTS := fsutiltest.WriteOutside(t,
		fmt.Sprintf("const id = \"virtual:canary/%s\"\n", fsutiltest.Marker))

	root := t.TempDir()
	fsutiltest.Symlink(t, root, "package.json", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "pkg-a/package.json", outsideJSON)
	fsutiltest.Symlink(t, root, "src/plugin.ts", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "src/leak.ts", outsideTS)
	// Registered by name alone (no read), so only the walk's own symlink skip
	// keeps it out of LibDirs.
	fsutiltest.Symlink(t, root, "app/svelte.config.js", outsideTS)

	var cfg Config
	fsutiltest.Within(t, func() { cfg = BuildConfig(root) })

	if got := fmt.Sprintf("%+v", cfg); strings.Contains(got, fsutiltest.Marker) || strings.Contains(got, "canary") {
		t.Fatalf("symlinked content reached Config: %s", got)
	}
	if len(cfg.Workspace) != 0 || len(cfg.VirtualModules) != 0 || len(cfg.LibDirs) != 0 {
		t.Fatalf("symlinked files must contribute nothing, got %+v", cfg)
	}
}

// TestBuildConfig_RealFilesStillRead guards the other direction: the refusal
// must not swallow the repo's own regular files.
func TestBuildConfig_RealFilesStillRead(t *testing.T) {
	root := t.TempDir()
	fsutiltest.WriteFile(t, root, "pkg-a/package.json", `{"name":"@acme/a"}`)
	fsutiltest.WriteFile(t, root, "pkg-a/vite.ts", "const id = \"virtual:acme/mod\"\n")
	cfg := BuildConfig(root)
	if cfg.Workspace["@acme/a"] != "pkg-a" || cfg.VirtualModules["virtual:acme/mod"] != "pkg-a" {
		t.Fatalf("regular files not read: %+v", cfg)
	}
}

// TestReadPackageManifest_RefusesNonRegular pins the read site itself, so it
// stays safe even if a caller other than the BuildConfig walk reaches it.
func TestReadPackageManifest_RefusesNonRegular(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, fmt.Sprintf(`{"name":"%s"}`, fsutiltest.Marker))
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "zero/package.json", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "out/package.json", outside)
	for _, rel := range []string{"zero/package.json", "out/package.json"} {
		fsutiltest.Within(t, func() {
			if name, _, ok := readPackageManifest(root, rel); ok || name != "" {
				t.Errorf("%s: manifest read through a symlink: name=%q ok=%v", rel, name, ok)
			}
		})
	}
}

// TestScanVirtualModules_RefusesNonRegular is the same pin for the 16 KiB scan.
func TestScanVirtualModules_RefusesNonRegular(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "const id = \"virtual:canary/x\"\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "zero.ts", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "out.ts", outside)
	out := map[string]string{}
	fsutiltest.Within(t, func() {
		scanVirtualModules(root, "zero.ts", out)
		scanVirtualModules(root, "out.ts", out)
	})
	if len(out) != 0 {
		t.Fatalf("virtual modules read through a symlink: %v", out)
	}
}
