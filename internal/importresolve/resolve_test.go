package importresolve_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/importresolve"
)

// mkResolver builds a Resolver with zero Config (no aliases).
// pkgDirs is a set of repo-relative directory paths (e.g. "web/src/lib").
// files is the set of all indexed file paths (repo-relative).
func mkResolver(pkgDirs []string, files []string) *importresolve.Resolver {
	pkgSet := make(map[string]struct{}, len(pkgDirs))
	for _, d := range pkgDirs {
		pkgSet[d] = struct{}{}
	}
	fileSet := make(map[string]struct{}, len(files))
	for _, f := range files {
		fileSet[f] = struct{}{}
	}
	return importresolve.New(pkgSet, fileSet, importresolve.Config{})
}

// mkResolverCfg builds a Resolver with an explicit Config.
func mkResolverCfg(pkgDirs []string, files []string, cfg importresolve.Config) *importresolve.Resolver {
	pkgSet := make(map[string]struct{}, len(pkgDirs))
	for _, d := range pkgDirs {
		pkgSet[d] = struct{}{}
	}
	fileSet := make(map[string]struct{}, len(files))
	for _, f := range files {
		fileSet[f] = struct{}{}
	}
	return importresolve.New(pkgSet, fileSet, cfg)
}

// TestResolve_GoExactMatch verifies that an import matching a pkgDir exactly
// resolves to that dir.
// Falsification: remove exact-match branch → test fails.
func TestResolve_GoExactMatch(t *testing.T) {
	t.Parallel()
	r := mkResolver([]string{"internal/util"}, nil)
	dir, ok := r.Resolve("internal/util", "cmd/main.go")
	if !ok {
		t.Fatal("expected ok=true for exact match")
	}
	if dir != "internal/util" {
		t.Errorf("dir = %q, want %q", dir, "internal/util")
	}
}

// TestResolve_GoLongestSuffixMatch verifies that when two pkgDirs share the same
// base name, the LONGEST suffix match wins (most-specific package).
// Falsification: use first-match instead of longest → "internal/b/util" might win
// instead of "internal/a/sub/util" depending on map iteration order.
func TestResolve_GoLongestSuffixMatch(t *testing.T) {
	t.Parallel()
	// Both "util" and "sub/util" suffix-match the import below; the longer (more
	// specific) one must win. Under the old first-match semantics map iteration
	// could return either, so this case genuinely discriminates longest-match.
	r := mkResolver([]string{"util", "sub/util"}, nil)
	dir, ok := r.Resolve("github.com/x/y/sub/util", "cmd/main.go")
	if !ok {
		t.Fatal("expected ok=true for suffix match")
	}
	if dir != "sub/util" {
		t.Errorf("dir = %q, want %q (longest suffix, not the shorter 'util')", dir, "sub/util")
	}
}

// TestResolve_TSRelativeExtensionless verifies "./chat" resolves to the container
// dir of chat.ts (i.e. same dir as the importer), not an external node.
// importingDir is filepath.Dir(relFile) — callers pre-compute it.
// Falsification: skip relative resolution → returns ("", false).
func TestResolve_TSRelativeExtensionless(t *testing.T) {
	t.Parallel()
	r := mkResolver(
		[]string{"web/src/lib"},
		[]string{"web/src/lib/app.ts", "web/src/lib/chat.ts"},
	)
	// importingDir = filepath.Dir("web/src/lib/app.ts") = "web/src/lib"
	dir, ok := r.Resolve("./chat", "web/src/lib")
	if !ok {
		t.Fatal("expected ok=true for extensionless relative import")
	}
	if dir != "web/src/lib" {
		t.Errorf("dir = %q, want %q", dir, "web/src/lib")
	}
}

// TestResolve_TSRelativeIndexDir verifies "./video" resolves to the container dir
// "web/src/lib/video" when web/src/lib/video/index.ts exists.
// importingDir is filepath.Dir(relFile) — callers pre-compute it.
// Falsification: skip index-dir probe → returns ("", false).
func TestResolve_TSRelativeIndexDir(t *testing.T) {
	t.Parallel()
	r := mkResolver(
		[]string{"web/src/lib", "web/src/lib/video"},
		[]string{"web/src/lib/app.ts", "web/src/lib/video/index.ts"},
	)
	// importingDir = filepath.Dir("web/src/lib/app.ts") = "web/src/lib"
	dir, ok := r.Resolve("./video", "web/src/lib")
	if !ok {
		t.Fatal("expected ok=true for index-dir relative import")
	}
	if dir != "web/src/lib/video" {
		t.Errorf("dir = %q, want %q", dir, "web/src/lib/video")
	}
}

// TestResolve_TSRelativeParent verifies "../util/fmt.ts" resolves to "web/src/util"
// (explicit extension, parent-crossing relative import).
// importingDir is filepath.Dir(relFile) — callers pre-compute it.
// Falsification: drop relative resolution → external node instead of container.
func TestResolve_TSRelativeParent(t *testing.T) {
	t.Parallel()
	r := mkResolver(
		[]string{"web/src/lib", "web/src/util"},
		[]string{"web/src/lib/app.ts", "web/src/util/fmt.ts"},
	)
	// importingDir = filepath.Dir("web/src/lib/app.ts") = "web/src/lib"
	dir, ok := r.Resolve("../util/fmt.ts", "web/src/lib")
	if !ok {
		t.Fatal("expected ok=true for parent-crossing explicit-ext relative import")
	}
	if dir != "web/src/util" {
		t.Errorf("dir = %q, want %q", dir, "web/src/util")
	}
}

// TestResolve_ExternalReturnsFalse verifies that an unresolvable import (no
// matching pkgDir, no relative file) returns ("", false).
// Falsification: always return true → external imports get incorrectly classified.
func TestResolve_ExternalReturnsFalse(t *testing.T) {
	t.Parallel()
	r := mkResolver([]string{"internal/util"}, nil)
	dir, ok := r.Resolve("react", "web/src/lib/app.ts")
	if ok {
		t.Errorf("expected ok=false for external import, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir for external import, got %q", dir)
	}
}

// TestResolve_SameBaseDistinctPath verifies that two dirs with the same base name
// ("docker" from "fleet/docker" vs "docker/client" from another module) are NOT
// conflated. Only the one whose suffix matches the import wins.
// Falsification: use base-only lookup without suffix check → wrong dir returned.
func TestResolve_SameBaseDistinctPath(t *testing.T) {
	t.Parallel()
	r := mkResolver(
		[]string{"internal/fleet/docker"},
		nil,
	)
	// Full module import for the local package.
	dir, ok := r.Resolve("github.com/anatolykoptev/go-code/internal/fleet/docker", "internal/fleet/fleet.go")
	if !ok {
		t.Fatal("expected ok=true for local fleet/docker import")
	}
	if dir != "internal/fleet/docker" {
		t.Errorf("dir = %q, want %q", dir, "internal/fleet/docker")
	}

	// External import with same base "docker" must NOT resolve to local fleet/docker.
	dir2, ok2 := r.Resolve("github.com/docker/docker/client", "internal/fleet/docker/driver.go")
	if ok2 {
		t.Errorf("external same-base import should not resolve to local, got dir=%q", dir2)
	}
}

// ---------------------------------------------------------------------------
// $lib alias tests
// ---------------------------------------------------------------------------

// TestResolve_LibAlias_FileMatch verifies that "$lib/i18n" with a LibDir of "web"
// resolves to "web/src/lib" when "web/src/lib/i18n.ts" exists in the file set.
// Falsification: comment out the $lib dispatch branch → returns ("", false).
func TestResolve_LibAlias_FileMatch(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{LibDirs: []string{"web"}}
	r := mkResolverCfg(
		[]string{"web/src/lib"},
		[]string{"web/src/lib/i18n.ts"},
		cfg,
	)
	dir, ok := r.Resolve("$lib/i18n", "web/src/routes/+page.svelte")
	if !ok {
		t.Fatal("expected ok=true for $lib alias with matching file")
	}
	if dir != "web/src/lib" {
		t.Errorf("dir = %q, want %q", dir, "web/src/lib")
	}
}

// TestResolve_LibAlias_DirIndex verifies that "$lib/video" resolves to
// "web/src/lib/video" when "web/src/lib/video/index.ts" exists.
// Falsification: remove index-dir probe from $lib resolution → returns ("", false).
func TestResolve_LibAlias_DirIndex(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{LibDirs: []string{"web"}}
	r := mkResolverCfg(
		[]string{"web/src/lib", "web/src/lib/video"},
		[]string{"web/src/lib/video/index.ts"},
		cfg,
	)
	dir, ok := r.Resolve("$lib/video", "web/src/routes/+page.svelte")
	if !ok {
		t.Fatal("expected ok=true for $lib alias with index-dir target")
	}
	if dir != "web/src/lib/video" {
		t.Errorf("dir = %q, want %q", dir, "web/src/lib/video")
	}
}

// TestResolve_LibAlias_ExactLib verifies that a bare "$lib" import (no subpath)
// resolves to "web/src/lib" when that dir is in pkgDirs.
// Falsification: skip exact-lib case → returns ("", false).
func TestResolve_LibAlias_ExactLib(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{LibDirs: []string{"web"}}
	r := mkResolverCfg(
		[]string{"web/src/lib"},
		nil,
		cfg,
	)
	dir, ok := r.Resolve("$lib", "web/src/routes/+page.svelte")
	if !ok {
		t.Fatal("expected ok=true for bare $lib import")
	}
	if dir != "web/src/lib" {
		t.Errorf("dir = %q, want %q", dir, "web/src/lib")
	}
}

// TestResolve_LibAlias_ZeroConfig verifies that $lib imports with zero Config
// fall through to external (ok=false). This guards the analyze path.
// Falsification: return true for $lib unconditionally → analyze gets false positives.
func TestResolve_LibAlias_ZeroConfig(t *testing.T) {
	t.Parallel()
	r := mkResolver([]string{"web/src/lib"}, []string{"web/src/lib/i18n.ts"})
	dir, ok := r.Resolve("$lib/i18n", "web/src/routes/+page.svelte")
	if ok {
		t.Errorf("expected ok=false with zero Config, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir with zero Config, got %q", dir)
	}
}

// ---------------------------------------------------------------------------
// @scope/pkg workspace alias tests
// ---------------------------------------------------------------------------

// TestResolve_WorkspaceAlias_PkgRoot verifies that "@oxpulse/mesh-core" with
// Workspace={"@oxpulse/mesh-core":"packages/mesh-core"} and that dir in pkgDirs
// resolves to "packages/mesh-core".
// Falsification: remove workspace dispatch → returns ("", false).
func TestResolve_WorkspaceAlias_PkgRoot(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{Workspace: map[string]string{"@oxpulse/mesh-core": "packages/mesh-core"}}
	r := mkResolverCfg(
		[]string{"packages/mesh-core"},
		nil,
		cfg,
	)
	dir, ok := r.Resolve("@oxpulse/mesh-core", "web/src/lib/app.ts")
	if !ok {
		t.Fatal("expected ok=true for workspace package root import")
	}
	if dir != "packages/mesh-core" {
		t.Errorf("dir = %q, want %q", dir, "packages/mesh-core")
	}
}

// TestResolve_WorkspaceAlias_Subpath verifies that "@oxpulse/mesh-core/sub"
// resolves to "packages/mesh-core/sub" when that path is in fileSet or pkgDirs.
// Falsification: drop subpath support → returns ("", false) or the wrong dir.
func TestResolve_WorkspaceAlias_Subpath(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{Workspace: map[string]string{"@oxpulse/mesh-core": "packages/mesh-core"}}
	r := mkResolverCfg(
		[]string{"packages/mesh-core", "packages/mesh-core/sub"},
		[]string{"packages/mesh-core/sub/index.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@oxpulse/mesh-core/sub", "web/src/lib/app.ts")
	if !ok {
		t.Fatal("expected ok=true for workspace subpath import")
	}
	if dir != "packages/mesh-core/sub" {
		t.Errorf("dir = %q, want %q", dir, "packages/mesh-core/sub")
	}
}

// TestResolve_WorkspaceAlias_ZeroConfig verifies that @scope/pkg imports with
// zero Config fall through to external (ok=false). This guards the analyze path.
// Falsification: return true for @-prefixed imports unconditionally → analyze false positives.
func TestResolve_WorkspaceAlias_ZeroConfig(t *testing.T) {
	t.Parallel()
	r := mkResolver([]string{"packages/mesh-core"}, nil)
	dir, ok := r.Resolve("@oxpulse/mesh-core", "web/src/lib/app.ts")
	if ok {
		t.Errorf("expected ok=false with zero Config for @scope/pkg, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir with zero Config, got %q", dir)
	}
}

// ---------------------------------------------------------------------------
// BuildConfig tests
// ---------------------------------------------------------------------------

// TestBuildConfig_RealDisk is the canonical BuildConfig test: it writes actual
// files to a temp directory on disk and calls BuildConfig(root), proving that
// the implementation walks the filesystem rather than reading from an in-memory
// map. This is the test that catches the BLOCKER (ingest drops .json so
// in.Files-based BuildConfig had an always-empty Workspace in production).
//
// Falsification (red-on-revert): if BuildConfig is reverted to the in.Files
// approach (taking map[string]string), this test will not compile. If
// node_modules exclusion is removed, Workspace["junk"] will appear and the
// assertion at the bottom fails.
func TestBuildConfig_RealDisk(t *testing.T) {
	t.Parallel()

	// Fixture layout:
	//   web/svelte.config.js               → LibDirs must contain "web"
	//   packages/mesh-core/package.json    → Workspace["@oxpulse/mesh-core"] = "packages/mesh-core"
	//   packages/mesh-core/index.ts        → a real source file (non-config)
	//   node_modules/junk/package.json     → must be IGNORED (node_modules exclusion)
	tmp := t.TempDir()

	writeFile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	writeFile("web/svelte.config.js", "export default {};")
	writeFile("packages/mesh-core/package.json", `{"name":"@oxpulse/mesh-core","version":"1.0.0"}`)
	writeFile("packages/mesh-core/index.ts", "export {};")
	writeFile("node_modules/junk/package.json", `{"name":"junk","version":"0.0.1"}`)

	// Call with the repo root — no in.Files map, just a path.
	cfg := importresolve.BuildConfig(tmp)

	// LibDirs must contain "web" (dir of svelte.config.js).
	foundWeb := false
	for _, d := range cfg.LibDirs {
		if d == "web" {
			foundWeb = true
		}
	}
	if !foundWeb {
		t.Errorf("LibDirs = %v, want to contain %q", cfg.LibDirs, "web")
	}

	// Workspace must map "@oxpulse/mesh-core" → "packages/mesh-core".
	if got, ok := cfg.Workspace["@oxpulse/mesh-core"]; !ok || got != "packages/mesh-core" {
		t.Errorf("Workspace[@oxpulse/mesh-core] = %q (ok=%v), want %q", got, ok, "packages/mesh-core")
	}

	// node_modules entry must be absent — path-segment exclusion.
	if _, found := cfg.Workspace["junk"]; found {
		t.Error("Workspace must not contain package.json entries from node_modules/ (path-segment skip)")
	}

	// Non-config source files must NOT appear in LibDirs.
	for _, d := range cfg.LibDirs {
		if d == "packages/mesh-core" {
			t.Errorf("LibDirs unexpectedly contains %q (should only be svelte.config dirs)", d)
		}
	}
}

// ---------------------------------------------------------------------------
// MAJOR 1: workspace alias must not return local when path not in pkgDirs/fileSet
// ---------------------------------------------------------------------------

// TestResolve_WorkspaceAlias_NotInPkgDirs_FallsThrough verifies that when a
// workspace entry maps "@scope/pkg" to a dir that is NOT in pkgDirs and NOT in
// fileSet, resolveWorkspaceAlias returns ("", false) so an external vertex is
// created rather than a silently-dropped edge.
//
// Before the fix the permissive branch returned (wsDir, true) even when wsDir
// was not a known package — callers assumed isLocal=true meant a vertex existed
// and skipped external-vertex creation, dropping the IMPORTS edge entirely.
//
// Falsification (red-on-revert): revert resolveWorkspaceAlias to the permissive
// "return wsDir, true" for package-root imports → this test fails because ok=true.
func TestResolve_WorkspaceAlias_NotInPkgDirs_FallsThrough(t *testing.T) {
	t.Parallel()
	// Workspace says "@external/pkg" lives at "packages/external-pkg", but that
	// dir is NOT in pkgDirs and there are no files under it in fileSet.
	cfg := importresolve.Config{Workspace: map[string]string{"@external/pkg": "packages/external-pkg"}}
	r := mkResolverCfg(
		[]string{"packages/mesh-core"}, // external-pkg NOT here
		nil,                            // no files either
		cfg,
	)
	dir, ok := r.Resolve("@external/pkg", "web/src/lib/app.ts")
	if ok {
		t.Errorf("expected ok=false when wsDir not in pkgDirs, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir when wsDir not in pkgDirs, got %q", dir)
	}
}
