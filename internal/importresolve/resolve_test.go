package importresolve_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/importresolve"
)

// mkResolver builds a Resolver from the given pkgDirs and file paths.
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
	return importresolve.New(pkgSet, fileSet)
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
