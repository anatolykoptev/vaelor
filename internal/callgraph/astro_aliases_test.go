package callgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// writeTSConfig writes a minimal tsconfig.json into dir with the given content.
func writeTSConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write tsconfig.json: %v", err)
	}
}

func TestLoadTSConfigAliases_TildeAlias(t *testing.T) {
	dir := t.TempDir()
	writeTSConfig(t, dir, `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"~/*": ["src/*"]
			}
		}
	}`)

	m := buildAliasMap(dir) // bypass cache for test isolation
	if got, ok := m["~/"]; !ok {
		t.Fatalf("alias '~/' not found in map; got %v", m)
	} else if got != "src" {
		t.Errorf("alias '~/' = %q, want %q", got, "src")
	}
}

func TestLoadTSConfigAliases_AtAlias(t *testing.T) {
	dir := t.TempDir()
	writeTSConfig(t, dir, `{
		"compilerOptions": {
			"paths": {
				"@/*": ["./src/*"]
			}
		}
	}`)

	m := buildAliasMap(dir)
	if got, ok := m["@/"]; !ok {
		t.Fatalf("alias '@/' not found; got %v", m)
	} else if got != "src" {
		t.Errorf("alias '@/' = %q, want %q", got, "src")
	}
}

func TestLoadTSConfigAliases_NoTSConfig(t *testing.T) {
	dir := t.TempDir()
	// No tsconfig.json → empty map, no panic.
	m := buildAliasMap(dir)
	if len(m) != 0 {
		t.Errorf("expected empty alias map for repo with no tsconfig, got %v", m)
	}
}

func TestLoadTSConfigAliases_WithComments(t *testing.T) {
	dir := t.TempDir()
	// tsconfig with // comments — common in real projects.
	writeTSConfig(t, dir, `{
		// This is a comment
		"compilerOptions": {
			"paths": {
				"~/*": ["src/*"] // tilde alias
			}
		}
	}`)

	m := buildAliasMap(dir)
	if _, ok := m["~/"]; !ok {
		t.Errorf("alias not parsed from tsconfig with comments; map: %v", m)
	}
}

// TestResolveTemplateRefs_AliasImport verifies that an alias import (~/) resolves
// to a USES edge when tsconfig.json declares the alias mapping.
func TestResolveTemplateRefs_AliasImport(t *testing.T) {
	// Write a tsconfig.json into a fresh temp dir so there is no cache pollution.
	dir := t.TempDir()
	writeTSConfig(t, dir, `{
		"compilerOptions": {
			"paths": {
				"~/*": ["src/*"]
			}
		}
	}`)

	src := []byte("---\nimport Foo from '~/components/Foo.astro'\n---\n<Foo />")
	refs := []preproc.TemplateRef{ref("Foo", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/pages/Home.astro", dir)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage for alias import, got %d", len(usages))
	}
	want := filepath.Join("src", "components", "Foo.astro")
	if usages[0].To != want {
		t.Errorf("usage.To = %q, want %q", usages[0].To, want)
	}
}

// TestResolveTemplateRefs_RelativeStillWorksNoAlias is a regression test:
// repos without any alias config (no tsconfig) must still resolve relative
// imports correctly, and the counter must not be incremented for bare specifiers.
func TestResolveTemplateRefs_RelativeStillWorksNoAlias(t *testing.T) {
	dir := t.TempDir()
	// No tsconfig.json in dir — alias map will be empty.

	src := []byte("---\nimport X from './c/X.astro'\n---\n<X />")
	refs := []preproc.TemplateRef{ref("X", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", dir)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage for relative import with no alias config, got %d", len(usages))
	}
	if usages[0].To != "src/c/X.astro" {
		t.Errorf("To = %q, want src/c/X.astro", usages[0].To)
	}
}

// TestResolveAlias_Match checks the resolveAlias helper directly.
func TestResolveAlias_Match(t *testing.T) {
	aliases := aliasMap{
		"~/": "src",
		"@/": "src",
	}
	got, ok := resolveAlias("~/components/Header.astro", aliases)
	if !ok {
		t.Fatal("expected match for ~/components/Header.astro")
	}
	want := filepath.Join("src", "components", "Header.astro")
	if got != want {
		t.Errorf("resolveAlias = %q, want %q", got, want)
	}
}

// TestResolveAlias_NoMatch verifies that bare specifiers do not match.
func TestResolveAlias_NoMatch(t *testing.T) {
	aliases := aliasMap{"~/": "src"}
	_, ok := resolveAlias("svelte", aliases)
	if ok {
		t.Error("expected no match for bare specifier 'svelte'")
	}
}

// TestResolveAlias_LongestPrefixWins verifies longest-prefix-wins semantics
// (regression for MAJOR-2: non-deterministic map iteration).
// "@ui/Button" must always resolve to "packages/ui/Button" (prefix "@ui/"),
// never "src/ui/Button" (prefix "@/").
func TestResolveAlias_LongestPrefixWins(t *testing.T) {
	aliases := aliasMap{
		"@/":   "src",
		"@ui/": "packages/ui",
	}
	want := filepath.Join("packages", "ui", "Button")
	for i := 0; i < 100; i++ {
		got, ok := resolveAlias("@ui/Button", aliases)
		if !ok {
			t.Fatalf("iter %d: expected match for @ui/Button", i)
		}
		if got != want {
			t.Fatalf("iter %d: resolveAlias(@ui/Button) = %q, want %q (longest-prefix-wins broken)", i, got, want)
		}
	}
}

// TestResolveAlias_WildcardTarget verifies that tsconfig wildcard paths like
// "@ui/*": ["packages/ui/*"] resolve correctly.
func TestResolveAlias_WildcardTarget(t *testing.T) {
	dir := t.TempDir()
	writeTSConfig(t, dir, `{
		"compilerOptions": {
			"paths": {
				"@ui/*": ["packages/ui/*"]
			}
		}
	}`)

	m := buildAliasMap(dir)
	got, ok := resolveAlias("@ui/Button", m)
	if !ok {
		t.Fatalf("expected match for @ui/Button; alias map: %v", m)
	}
	want := filepath.Join("packages", "ui", "Button")
	if got != want {
		t.Errorf("resolveAlias(@ui/Button) = %q, want %q", got, want)
	}
}

// TestBuildAliasMap_TrailingComma verifies that tsconfig with trailing commas
// (common in real projects) is parsed gracefully without panicking.
func TestBuildAliasMap_TrailingComma(t *testing.T) {
	dir := t.TempDir()
	writeTSConfig(t, dir, `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"~/*": ["src/*"],
			},
		},
	}`)

	// Must not panic; result may be empty or partial but never a crash.
	m := buildAliasMap(dir)
	// With trailing-comma stripping the map should be populated.
	if _, ok := m["~/"]; !ok {
		t.Errorf("trailing-comma tsconfig should parse alias '~/'; got map %v", m)
	}
}

// TestBuildAliasMap_ExtendsChainBaseUrl verifies that baseUrl declared in a
// base tsconfig file is resolved relative to THAT file's directory, not the
// extending file's directory.
func TestBuildAliasMap_ExtendsChainBaseUrl(t *testing.T) {
	root := t.TempDir()
	// Subdirectory holds the base tsconfig.
	baseDir := filepath.Join(root, "config")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// base tsconfig declares baseUrl: "." which means baseDir.
	baseTS := filepath.Join(baseDir, "tsconfig.base.json")
	if err := os.WriteFile(baseTS, []byte(`{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"@/*": ["src/*"]
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Root tsconfig extends the base.
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{
		"extends": "./config/tsconfig.base.json"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := buildAliasMap(root)
	got, ok := m["@/"]
	if !ok {
		t.Fatalf("expected '@/' alias from extended base tsconfig; map: %v", m)
	}
	// baseUrl "." in baseDir → target "src" relative to baseDir, which is
	// config/src relative to root.
	want := filepath.Join("config", "src")
	if got != want {
		t.Errorf("alias '@/' = %q, want %q (baseUrl must resolve from base file dir)", got, want)
	}
}

// TestBuildAliasMap_CycleGuard verifies that mutually-extending tsconfig files
// do not cause infinite recursion / stack overflow.
func TestBuildAliasMap_CycleGuard(t *testing.T) {
	root := t.TempDir()

	// tsconfig.json extends tsconfig.base.json
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{
		"extends": "./tsconfig.base.json",
		"compilerOptions": {
			"paths": { "~/*": ["src/*"] }
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// tsconfig.base.json extends tsconfig.json — mutual cycle
	if err := os.WriteFile(filepath.Join(root, "tsconfig.base.json"), []byte(`{
		"extends": "./tsconfig.json"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Must not panic or stack-overflow.
	m := buildAliasMap(root)
	// The tilde alias from tsconfig.json should still be picked up.
	if _, ok := m["~/"]; !ok {
		t.Errorf("expected '~/' alias despite cycle; map: %v", m)
	}
}

// TestBuildAliasMap_StaleCacheInvalidation verifies that modifying tsconfig
// (bumping its mtime) causes loadTSConfigAliases to return fresh aliases
// rather than the stale cached map (regression for MAJOR-3).
func TestBuildAliasMap_StaleCacheInvalidation(t *testing.T) {
	root := t.TempDir()

	// Write initial tsconfig with "~/" alias.
	writeTSConfig(t, root, `{
		"compilerOptions": {
			"paths": { "~/*": ["src/*"] }
		}
	}`)

	m1 := loadTSConfigAliases(root)
	if _, ok := m1["~/"]; !ok {
		t.Fatalf("initial alias '~/' not found; map: %v", m1)
	}

	// Overwrite tsconfig with a different alias and bump mtime.
	time.Sleep(2 * time.Millisecond) // ensure mtime differs
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{
		"compilerOptions": {
			"paths": { "@/*": ["app/*"] }
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Touch to guarantee OS mtime granularity doesn't swallow the update.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(root, "tsconfig.json"), now, now); err != nil {
		t.Fatal(err)
	}

	m2 := loadTSConfigAliases(root)
	if _, ok := m2["@/"]; !ok {
		t.Fatalf("updated alias '@/' not found after tsconfig change; map: %v (stale cache?)", m2)
	}
	if _, ok := m2["~/"]; ok {
		t.Errorf("stale alias '~/' still present after tsconfig change (cache not invalidated); map: %v", m2)
	}
}

// TestResolveAlias_PathTraversalGuard verifies that an alias target that
// resolves outside the repo root is rejected (OWASP path-traversal parity).
func TestResolveAlias_PathTraversalGuard(t *testing.T) {
	// Alias maps "bad/" to a deeply relative path that would escape the root.
	aliases := aliasMap{
		"bad/": "../../../etc",
	}
	_, ok := resolveAlias("bad/passwd", aliases)
	if ok {
		t.Error("expected resolveAlias to reject path that escapes repo root")
	}
}

// TestAstroConfigFallback verifies that aliases are loaded from astro.config.mjs
// when no tsconfig.json is present (fallback branch).
func TestAstroConfigFallback(t *testing.T) {
	dir := t.TempDir()
	// No tsconfig.json — only astro.config.mjs with a simple alias.
	if err := os.WriteFile(filepath.Join(dir, "astro.config.mjs"), []byte(`
import { defineConfig } from 'astro/config';
export default defineConfig({
  vite: {
    resolve: {
      alias: {
        '~/': './src/',
      }
    }
  }
});
`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := buildAliasMap(dir)
	if _, ok := m["~/"]; !ok {
		t.Errorf("expected '~/' alias from astro.config.mjs fallback; map: %v", m)
	}
}
