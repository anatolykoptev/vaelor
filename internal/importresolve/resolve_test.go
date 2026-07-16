package importresolve_test

import (
	"encoding/json"
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

// TestResolve_WorkspaceAlias_PkgRoot verifies that "@acme/mesh-core" with
// Workspace={"@acme/mesh-core":"packages/mesh-core"} and that dir in pkgDirs
// resolves to "packages/mesh-core".
// Falsification: remove workspace dispatch → returns ("", false).
func TestResolve_WorkspaceAlias_PkgRoot(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{Workspace: map[string]string{"@acme/mesh-core": "packages/mesh-core"}}
	r := mkResolverCfg(
		[]string{"packages/mesh-core"},
		nil,
		cfg,
	)
	dir, ok := r.Resolve("@acme/mesh-core", "web/src/lib/app.ts")
	if !ok {
		t.Fatal("expected ok=true for workspace package root import")
	}
	if dir != "packages/mesh-core" {
		t.Errorf("dir = %q, want %q", dir, "packages/mesh-core")
	}
}

// TestResolve_WorkspaceAlias_Subpath verifies that "@acme/mesh-core/sub"
// resolves to "packages/mesh-core/sub" when that path is in fileSet or pkgDirs.
// Falsification: drop subpath support → returns ("", false) or the wrong dir.
func TestResolve_WorkspaceAlias_Subpath(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{Workspace: map[string]string{"@acme/mesh-core": "packages/mesh-core"}}
	r := mkResolverCfg(
		[]string{"packages/mesh-core", "packages/mesh-core/sub"},
		[]string{"packages/mesh-core/sub/index.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@acme/mesh-core/sub", "web/src/lib/app.ts")
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
	dir, ok := r.Resolve("@acme/mesh-core", "web/src/lib/app.ts")
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
	//   packages/mesh-core/package.json    → Workspace["@acme/mesh-core"] = "packages/mesh-core"
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
	writeFile("packages/mesh-core/package.json", `{"name":"@acme/mesh-core","version":"1.0.0"}`)
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

	// Workspace must map "@acme/mesh-core" → "packages/mesh-core".
	if got, ok := cfg.Workspace["@acme/mesh-core"]; !ok || got != "packages/mesh-core" {
		t.Errorf("Workspace[@acme/mesh-core] = %q (ok=%v), want %q", got, ok, "packages/mesh-core")
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

// ---------------------------------------------------------------------------
// Workspace exports subpath rewrite tests (#422)
// ---------------------------------------------------------------------------

// TestResolve_WorkspaceAlias_ExportsWildcardSrcRewrite verifies that
// "@guide/ui/components/HomeHero.astro" resolves when @guide/ui's package.json
// declares {"exports": {"./*": "./src/*"}} and the file lives under src/.
// This is the acme-guide @guide/ui layout — without the exports rewrite the
// bare wsDir+subpath probe misses (file is at packages/ui/src/components/...,
// not packages/ui/components/...) and the IMPORTS edge is dropped.
// Falsification: revert resolveViaExports → returns ("", false).
func TestResolve_WorkspaceAlias_ExportsWildcardSrcRewrite(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace:        map[string]string{"@guide/ui": "packages/ui"},
		WorkspaceExports: map[string]map[string]string{"@guide/ui": {"*": "src/*"}},
	}
	r := mkResolverCfg(
		[]string{"packages/ui/src/components"},
		[]string{"packages/ui/src/components/HomeHero.astro"},
		cfg,
	)
	dir, ok := r.Resolve("@guide/ui/components/HomeHero.astro", "packages/pages/src/entry/home.astro")
	if !ok {
		t.Fatal("expected ok=true for @guide/ui subpath via exports ./* → ./src/* rewrite")
	}
	if dir != "packages/ui/src/components" {
		t.Errorf("dir = %q, want %q", dir, "packages/ui/src/components")
	}
}

// TestResolve_WorkspaceAlias_ExportsMultiSegmentWildcard verifies that a
// multi-segment wildcard key like "components/*" → "src/components/*" (the real
// @guide/ui package.json shape) resolves "@guide/ui/components/HomeHero.astro"
// to packages/ui/src/components. This is the actual production layout — the
// bare "./*" idiom is a simplification; @guide/ui scopes wildcards per top-level
// dir (components/*, styles/*).
// Falsification: only support key=="*" (single-segment) → multi-segment miss.
func TestResolve_WorkspaceAlias_ExportsMultiSegmentWildcard(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace: map[string]string{"@guide/ui": "packages/ui"},
		WorkspaceExports: map[string]map[string]string{
			"@guide/ui": {
				"components/*": "src/components/*",
				"styles/*":     "src/styles/*",
				"ranking":      "src/ranking.ts",
			},
		},
	}
	r := mkResolverCfg(
		[]string{"packages/ui/src/components", "packages/ui/src/styles"},
		[]string{
			"packages/ui/src/components/HomeHero.astro",
			"packages/ui/src/components/PlaceCard.astro",
			"packages/ui/src/styles/card.css",
		},
		cfg,
	)
	// components/* → src/components/HomeHero.astro
	dir, ok := r.Resolve("@guide/ui/components/HomeHero.astro", "packages/pages/src/entry/home.astro")
	if !ok {
		t.Fatal("expected ok=true for multi-segment wildcard components/* → src/components/*")
	}
	if dir != "packages/ui/src/components" {
		t.Errorf("dir = %q, want %q", dir, "packages/ui/src/components")
	}
	// styles/* → src/styles/card.css (non-astro extension exercises the explicit-ext probe)
	dir2, ok2 := r.Resolve("@guide/ui/styles/card.css", "packages/pages/src/entry/home.astro")
	if !ok2 {
		t.Fatal("expected ok=true for multi-segment wildcard styles/* → src/styles/*")
	}
	if dir2 != "packages/ui/src/styles" {
		t.Errorf("dir = %q, want %q", dir2, "packages/ui/src/styles")
	}
}

// TestResolve_WorkspaceAlias_ExportsWildcardExtensionless verifies the wildcard
// rewrite also covers extensionless subpath imports (e.g. "@guide/ui/ranking"
// where the file is packages/ui/src/ranking.ts). The rewritten subpath is probed
// with the standard extensionless-file strategy in resolveAbsSubpath.
// Falsification: drop the wildcard branch → returns ("", false).
func TestResolve_WorkspaceAlias_ExportsWildcardExtensionless(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace:        map[string]string{"@guide/ui": "packages/ui"},
		WorkspaceExports: map[string]map[string]string{"@guide/ui": {"*": "src/*"}},
	}
	r := mkResolverCfg(
		[]string{"packages/ui/src"},
		[]string{"packages/ui/src/ranking.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@guide/ui/ranking", "packages/pages/src/entry/home.astro")
	if !ok {
		t.Fatal("expected ok=true for extensionless @guide/ui subpath via exports rewrite")
	}
	if dir != "packages/ui/src" {
		t.Errorf("dir = %q, want %q", dir, "packages/ui/src")
	}
}

// TestResolve_WorkspaceAlias_ExportsExactKey verifies that an exact (non-wildcard)
// exports key is honored: {"./foo": "./bar.ts"} rewrites "@scope/pkg/foo" to
// probe wsDir/bar.ts.
// Falsification: only consult the "*" wildcard → exact-key miss returns false.
func TestResolve_WorkspaceAlias_ExportsExactKey(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace:        map[string]string{"@scope/pkg": "packages/pkg"},
		WorkspaceExports: map[string]map[string]string{"@scope/pkg": {"foo": "bar.ts"}},
	}
	r := mkResolverCfg(
		[]string{"packages/pkg"},
		[]string{"packages/pkg/bar.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@scope/pkg/foo", "apps/app/src/main.ts")
	if !ok {
		t.Fatal("expected ok=true for exact-key exports rewrite")
	}
	if dir != "packages/pkg" {
		t.Errorf("dir = %q, want %q", dir, "packages/pkg")
	}
}

// TestResolve_WorkspaceAlias_ExportsBareStringRoot verifies that a bare-string
// "exports": "./index.ts" (the package main entry) is normalized to key "" and
// resolves a bare "@scope/pkg" import to the index file's container dir.
// Falsification: skip the "" key in parseExports → bare import misses.
func TestResolve_WorkspaceAlias_ExportsBareStringRoot(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace:        map[string]string{"@scope/pkg": "packages/pkg"},
		WorkspaceExports: map[string]map[string]string{"@scope/pkg": {"": "index.ts"}},
	}
	r := mkResolverCfg(
		[]string{"packages/pkg"},
		[]string{"packages/pkg/index.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@scope/pkg", "apps/app/src/main.ts")
	if !ok {
		t.Fatal("expected ok=true for bare @scope/pkg via exports \"\" → index.ts")
	}
	if dir != "packages/pkg" {
		t.Errorf("dir = %q, want %q", dir, "packages/pkg")
	}
}

// TestResolve_WorkspaceAlias_ExportsRewriteStillMisses verifies that when the
// exports rewrite produces a path that does not exist in fileSet/pkgDirs, the
// resolver returns ("", false) so an external vertex is created — no false
// local. Guards against silently-dropped edges from a stale exports entry.
// Falsification: return wsDir unconditionally on any exports match → false local.
func TestResolve_WorkspaceAlias_ExportsRewriteStillMisses(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace:        map[string]string{"@scope/pkg": "packages/pkg"},
		WorkspaceExports: map[string]map[string]string{"@scope/pkg": {"*": "src/*"}},
	}
	// No files under packages/pkg/src/ — the rewritten probe must miss.
	r := mkResolverCfg(
		[]string{"packages/pkg"},
		nil,
		cfg,
	)
	dir, ok := r.Resolve("@scope/pkg/components/Missing.astro", "apps/app/src/main.ts")
	if ok {
		t.Errorf("expected ok=false when exports-rewritten path does not exist, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir on miss, got %q", dir)
	}
}

// TestResolve_WorkspaceAlias_NoExportsPreservesOldBehavior verifies that a
// workspace package with no WorkspaceExports entry still resolves via the bare
// wsDir+subpath probe (the pre-fix fast path). Guards against the exports
// dispatch breaking packages that put files directly under the package root.
// Falsification: make resolveViaExports mandatory → old-style packages miss.
func TestResolve_WorkspaceAlias_NoExportsPreservesOldBehavior(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		Workspace: map[string]string{"@scope/pkg": "packages/pkg"},
		// No WorkspaceExports — packages/pkg has files directly under it.
	}
	r := mkResolverCfg(
		[]string{"packages/pkg/sub"},
		[]string{"packages/pkg/sub/index.ts"},
		cfg,
	)
	dir, ok := r.Resolve("@scope/pkg/sub", "apps/app/src/main.ts")
	if !ok {
		t.Fatal("expected ok=true for workspace subpath without exports (pre-fix path)")
	}
	if dir != "packages/pkg/sub" {
		t.Errorf("dir = %q, want %q", dir, "packages/pkg/sub")
	}
}

// ---------------------------------------------------------------------------
// parseExports / BuildConfig exports-parsing tests
// ---------------------------------------------------------------------------

// TestParseExports_WildcardMap verifies the {"./*":"./src/*"} idiom normalizes
// to {"*":"src/*"} (leading "./" stripped on both sides, forward slashes).
// Falsification: drop the leading-"./" strip → key stays "./*" and never matches.
func TestParseExports_WildcardMap(t *testing.T) {
	t.Parallel()
	got := importresolve.ParseExports(json.RawMessage(`{"./*":"./src/*"}`))
	want := map[string]string{"*": "src/*"}
	if !mapsEqual(got, want) {
		t.Errorf("ParseExports(./* → ./src/*) = %v, want %v", got, want)
	}
}

// TestParseExports_ConditionObject verifies that a condition-object value
// ({"import": ..., "require": ...}) is flattened by preferring "import".
// Falsification: pick "require" first → wrong target for ESM-first packages.
func TestParseExports_ConditionObject(t *testing.T) {
	t.Parallel()
	got := importresolve.ParseExports(json.RawMessage(`{"./foo":{"import":"./esm/foo.mjs","require":"./cjs/foo.cjs"}}`))
	want := map[string]string{"foo": "esm/foo.mjs"}
	if !mapsEqual(got, want) {
		t.Errorf("ParseExports(condition obj) = %v, want %v (import preferred)", got, want)
	}
}

// TestParseExports_ArrayValue verifies that an array value yields its first
// string target (with fallback into nested condition objects).
// Falsification: only accept direct string values → array form returns nil.
func TestParseExports_ArrayValue(t *testing.T) {
	t.Parallel()
	got := importresolve.ParseExports(json.RawMessage(`{"./foo":["./a.ts",{"require":"./b.cjs"}]}`))
	want := map[string]string{"foo": "a.ts"}
	if !mapsEqual(got, want) {
		t.Errorf("ParseExports(array) = %v, want %v (first string)", got, want)
	}
}

// TestParseExports_BareString verifies the bare-string "exports": "./index.ts"
// form normalizes to {"": "index.ts"} (root entry).
// Falsification: only accept map form → bare string returns nil, root import misses.
func TestParseExports_BareString(t *testing.T) {
	t.Parallel()
	got := importresolve.ParseExports(json.RawMessage(`"./index.ts"`))
	want := map[string]string{"": "index.ts"}
	if !mapsEqual(got, want) {
		t.Errorf("ParseExports(bare string) = %v, want %v", got, want)
	}
}

// TestParseExports_Empty verifies that empty/absent input returns nil (no
// exports entry) so callers skip the exports dispatch entirely.
func TestParseExports_Empty(t *testing.T) {
	t.Parallel()
	if got := importresolve.ParseExports(nil); got != nil {
		t.Errorf("ParseExports(nil) = %v, want nil", got)
	}
	if got := importresolve.ParseExports(json.RawMessage(``)); got != nil {
		t.Errorf("ParseExports(empty) = %v, want nil", got)
	}
}

// TestBuildConfig_ReadsExports verifies that BuildConfig populates
// Config.WorkspaceExports from a real package.json on disk. This is the
// on-disk counterpart to TestBuildConfig_RealDisk — proves the walk reads
// the exports field, not just the name.
// Falsification: revert readPackageManifest to readPackageName → WorkspaceExports empty.
func TestBuildConfig_ReadsExports(t *testing.T) {
	t.Parallel()
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
	writeFile("packages/ui/package.json", `{"name":"@guide/ui","exports":{"./*":"./src/*"}}`)
	writeFile("packages/ui/src/components/HomeHero.astro", "---\n---")

	cfg := importresolve.BuildConfig(tmp)
	exp, ok := cfg.WorkspaceExports["@guide/ui"]
	if !ok {
		t.Fatal("WorkspaceExports[@guide/ui] missing — BuildConfig did not read exports")
	}
	if got := exp["*"]; got != "src/*" {
		t.Errorf("WorkspaceExports[@guide/ui][*] = %q, want %q", got, "src/*")
	}
}

// mapsEqual is a small helper for comparing two map[string]string without
// pulling in reflect.DeepEqual (which would also work but reads less clearly
// in test failure output).
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Virtual module stopgap tests (#423)
// ---------------------------------------------------------------------------

// TestResolve_VirtualModule_ResolvesToDefiningPackage verifies that a
// "virtual:guide/content" import resolves to the defining package's dir when
// cfg.VirtualModules maps it and that dir is in pkgDirs. This is the approach-2
// stopgap — the edge is package-to-package, not to the specific re-exported file.
// Falsification: revert the virtual: dispatch → returns ("", false).
func TestResolve_VirtualModule_ResolvesToDefiningPackage(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		VirtualModules: map[string]string{
			"virtual:guide/content": "packages/pages/src",
			"virtual:guide/layout":  "packages/pages/src",
			"virtual:guide/i18n":    "packages/pages/src",
		},
	}
	r := mkResolverCfg(
		[]string{"packages/pages/src", "packages/pages/src/entry"},
		[]string{"packages/pages/src/entry/home.astro"},
		cfg,
	)
	dir, ok := r.Resolve("virtual:guide/content", "packages/pages/src/entry/home.astro")
	if !ok {
		t.Fatal("expected ok=true for virtual:guide/content via VirtualModules stopgap")
	}
	if dir != "packages/pages/src" {
		t.Errorf("dir = %q, want %q (defining package dir)", dir, "packages/pages/src")
	}
}

// TestResolve_VirtualModule_ZeroConfig verifies that virtual: imports with zero
// Config fall through to external (ok=false). Guards the analyze path.
// Falsification: return true for virtual: unconditionally → analyze false positives.
func TestResolve_VirtualModule_ZeroConfig(t *testing.T) {
	t.Parallel()
	r := mkResolver([]string{"packages/pages/src"}, nil)
	dir, ok := r.Resolve("virtual:guide/content", "packages/pages/src/entry/home.astro")
	if ok {
		t.Errorf("expected ok=false with zero Config for virtual:, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir with zero Config, got %q", dir)
	}
}

// TestResolve_VirtualModule_NotInPkgDirs_FallsThrough verifies that when the
// defining dir is NOT in pkgDirs (e.g. the package was removed but the virtual
// id still appears in a consumer), the resolver returns ("", false) so an
// external vertex is created — no silently-dropped edge.
// Falsification: return dir unconditionally → false local, dropped edge.
func TestResolve_VirtualModule_NotInPkgDirs_FallsThrough(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		VirtualModules: map[string]string{"virtual:guide/content": "packages/removed/src"},
	}
	// packages/removed/src is NOT in pkgDirs.
	r := mkResolverCfg([]string{"packages/pages/src"}, nil, cfg)
	dir, ok := r.Resolve("virtual:guide/content", "packages/pages/src/entry/home.astro")
	if ok {
		t.Errorf("expected ok=false when defining dir not in pkgDirs, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir when defining dir not in pkgDirs, got %q", dir)
	}
}

// TestResolve_VirtualModule_UnknownId verifies that a virtual: import not in
// the VirtualModules map returns ("", false) — falls through to external.
func TestResolve_VirtualModule_UnknownId(t *testing.T) {
	t.Parallel()
	cfg := importresolve.Config{
		VirtualModules: map[string]string{"virtual:guide/content": "packages/pages/src"},
	}
	r := mkResolverCfg([]string{"packages/pages/src"}, nil, cfg)
	dir, ok := r.Resolve("virtual:unknown/thing", "packages/pages/src/entry/home.astro")
	if ok {
		t.Errorf("expected ok=false for unknown virtual id, got dir=%q", dir)
	}
	if dir != "" {
		t.Errorf("expected empty dir for unknown virtual id, got %q", dir)
	}
}

// TestBuildConfig_ScansVirtualModules verifies that BuildConfig populates
// Config.VirtualModules by scanning TS/JS source files for virtual: id string
// literals. Proves the walk reads file contents and extracts the ids.
// Falsification: revert scanVirtualModules → VirtualModules empty.
func TestBuildConfig_ScansVirtualModules(t *testing.T) {
	t.Parallel()
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
	// The defining file — a Vite plugin with virtual module ids in const declarations.
	writeFile("packages/pages/src/integration.ts", `
const VIRTUAL_CONTENT = 'virtual:guide/content';
const VIRTUAL_LAYOUT = 'virtual:guide/layout';
const VIRTUAL_I18N = "virtual:guide/i18n";
`)
	// A consumer .astro file — should NOT be scanned (not a .ts/.js file).
	writeFile("packages/pages/src/entry/home.astro", `---
import { content } from 'virtual:guide/content';
---`)

	cfg := importresolve.BuildConfig(tmp)
	if got, ok := cfg.VirtualModules["virtual:guide/content"]; !ok || got != "packages/pages/src" {
		t.Errorf("VirtualModules[virtual:guide/content] = %q (ok=%v), want %q", got, ok, "packages/pages/src")
	}
	if got, ok := cfg.VirtualModules["virtual:guide/layout"]; !ok || got != "packages/pages/src" {
		t.Errorf("VirtualModules[virtual:guide/layout] = %q (ok=%v), want %q", got, ok, "packages/pages/src")
	}
	if got, ok := cfg.VirtualModules["virtual:guide/i18n"]; !ok || got != "packages/pages/src" {
		t.Errorf("VirtualModules[virtual:guide/i18n] = %q (ok=%v), want %q", got, ok, "packages/pages/src")
	}
}

// TestBuildConfig_VirtualModules_NoAstroConsumers verifies that .astro consumer
// files do NOT contribute to VirtualModules — only TS/JS definers are scanned.
// Without this guard, a consumer .astro in a different package would register
// the wrong defining dir.
// Falsification: scan .astro files → consumer's dir wins over definer's dir.
func TestBuildConfig_VirtualModules_NoAstroConsumers(t *testing.T) {
	t.Parallel()
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
	// Definer in packages/pages/src.
	writeFile("packages/pages/src/integration.ts", `const V = 'virtual:guide/content';`)
	// Consumer in a DIFFERENT package (apps/piter/src/entry).
	writeFile("apps/piter/src/entry/home.astro", `import { content } from 'virtual:guide/content';`)

	cfg := importresolve.BuildConfig(tmp)
	got, ok := cfg.VirtualModules["virtual:guide/content"]
	if !ok {
		t.Fatal("VirtualModules[virtual:guide/content] missing — definer .ts not scanned")
	}
	if got != "packages/pages/src" {
		t.Errorf("VirtualModules[virtual:guide/content] = %q, want %q (definer dir, not consumer dir)", got, "packages/pages/src")
	}
}
