package callgraph

import (
	"encoding/json"
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

	m, _, _ := buildAliasMap(dir) // bypass cache for test isolation
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

	m, _, _ := buildAliasMap(dir)
	if got, ok := m["@/"]; !ok {
		t.Fatalf("alias '@/' not found; got %v", m)
	} else if got != "src" {
		t.Errorf("alias '@/' = %q, want %q", got, "src")
	}
}

func TestLoadTSConfigAliases_NoTSConfig(t *testing.T) {
	dir := t.TempDir()
	// No tsconfig.json → empty map, no panic.
	m, _, _ := buildAliasMap(dir)
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

	m, _, _ := buildAliasMap(dir)
	if _, ok := m["~/"]; !ok {
		t.Errorf("alias not parsed from tsconfig with comments; map: %v", m)
	}
}

// TestResolveTemplateRefs_AliasImport verifies that an alias import (~/) resolves
// to a USES edge when tsconfig.json declares the alias mapping and the target file
// exists on disk.
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
	// Create the target file — alias resolution now checks disk existence.
	if err := os.MkdirAll(filepath.Join(dir, "src", "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "components", "Foo.astro"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	aliasCache.Delete(dir)

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

	m, _, _ := buildAliasMap(dir)
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
	m, _, _ := buildAliasMap(dir)
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

	m, _, _ := buildAliasMap(root)
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
	m, _, _ := buildAliasMap(root)
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

	m, _, _ := buildAliasMap(dir)
	if _, ok := m["~/"]; !ok {
		t.Errorf("expected '~/' alias from astro.config.mjs fallback; map: %v", m)
	}
}

// TestBuildAliasMap_SubdirBaseStaleCacheInvalidation is a regression test for
// the subdir-base mtime staleness class (turborepo-monorepo scenario).
// When tsconfig.json extends config/tsconfig.base.json, editing the base file
// must cause loadTSConfigAliases to rebuild the cache on the next call.
//
// This test exercises the files-list approach: buildAliasMap records every file
// in the extends chain; loadTSConfigAliases re-stats those files, not just the
// root-level ones. Without this, touching config/tsconfig.base.json is invisible
// to a root-only stat check and the stale alias map persists until process restart.
//
// Red-on-revert: revert to aliasCacheKey{root, latestTSConfigMtime(root)} and
// this test fails because latestTSConfigMtime only stats the root tsconfig.json
// which has not changed.
func TestBuildAliasMap_SubdirBaseStaleCacheInvalidation(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	baseTS := filepath.Join(configDir, "tsconfig.base.json")
	if err := os.WriteFile(baseTS, []byte(`{
		"compilerOptions": {
			"paths": { "~/*": ["src/*"] }
		}
	}`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{
		"extends": "./config/tsconfig.base.json"
	}`), 0o644); err != nil {
		t.Fatalf("write root tsconfig: %v", err)
	}

	// Warm the cache.
	aliasCache.Delete(root)
	m1 := loadTSConfigAliases(root)
	if _, ok := m1["~/"]; !ok {
		t.Fatalf("initial load: alias '~/' not found; got %v", m1)
	}

	// Update ONLY the subdir base — root tsconfig.json is NOT touched.
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(baseTS, []byte(`{
		"compilerOptions": {
			"paths": { "~/*": ["src/*"], "newAlias/*": ["lib/*"] }
		}
	}`), 0o644); err != nil {
		t.Fatalf("update base: %v", err)
	}
	if err := os.Chtimes(baseTS, future, future); err != nil {
		t.Fatalf("chtimes base: %v", err)
	}

	// Second load must rebuild because the subdir base mtime advanced.
	m2 := loadTSConfigAliases(root)
	if _, ok := m2["newAlias/"]; !ok {
		t.Errorf("subdir-base edit not detected: cache not invalidated; got %v", m2)
	}
}

// TestBuildAliasMap_AstroConfigMtimeInvalidation verifies that editing
// astro.config.mjs (Astro-only repo without tsconfig aliases) causes the cached
// alias map to be rebuilt on the next loadTSConfigAliases call.
//
// Red-on-revert: remove astro.config path from files-list tracking in
// parseAstroConfigAliases and this test fails because astro.config mtime is not
// checked on re-entry.
func TestBuildAliasMap_AstroConfigMtimeInvalidation(t *testing.T) {
	dir := t.TempDir()
	astroConfig := filepath.Join(dir, "astro.config.mjs")
	// One alias per line so parseSimpleAliasLine can recognise them.
	if err := os.WriteFile(astroConfig, []byte(`export default defineConfig({
  vite: { resolve: { alias: {
    '~/': './src/',
  } } }
})
`), 0o644); err != nil {
		t.Fatalf("write astro.config: %v", err)
	}

	aliasCache.Delete(dir)
	m1 := loadTSConfigAliases(dir)
	if _, ok := m1["~/"]; !ok {
		t.Fatalf("initial load: alias '~/' not found; got %v", m1)
	}

	// Update astro.config and advance mtime.
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(astroConfig, []byte(`export default defineConfig({
  vite: { resolve: { alias: {
    '~/': './src/',
    '@/': './src/',
  } } }
})
`), 0o644); err != nil {
		t.Fatalf("update astro.config: %v", err)
	}
	if err := os.Chtimes(astroConfig, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	m2 := loadTSConfigAliases(dir)
	if _, ok := m2["@/"]; !ok {
		t.Errorf("astro.config edit not detected: cache not invalidated; got %v", m2)
	}
}

// TestBuildAliasMap_TrailingCommaInStringValue pins the known limitation of the
// trailingCommaRe regexp: a comma that appears inside a string value immediately
// before a closing bracket (e.g. "key": "a,]") is incorrectly stripped.
//
// This limitation is acceptable because tsconfig compilerOptions.paths values
// are filesystem paths and never contain literal ] or } characters. The test
// documents the current behaviour so any future fix has a regression guard.
//
// See the trailingCommaRe godoc for the full caveat.
func TestBuildAliasMap_TrailingCommaInStringValue(t *testing.T) {
	// Test the regexp directly to document the known limitation.
	input := `{"key": "a,]"}`
	// The regexp sees , followed by ] inside the string and strips the comma.
	got := string(trailingCommaRe.ReplaceAll([]byte(input), []byte("$1")))
	// Document: the comma inside the string IS stripped (known limitation).
	// If a future implementation preserves it, update the want string to
	// `{"key": "a,]"}` and add a JSON round-trip assertion.
	want := `{"key": "a]"}`
	if got != want {
		t.Logf("trailingCommaRe: got %q, documented limited behaviour %q (OK if stricter)", got, want)
	}

	// The common-case assertion: structural trailing commas must be stripped and
	// the result must be valid JSON (this is what really matters for tsconfig).
	validInput := `{"compilerOptions": {"paths": {"~/*": ["src/*",]}}}`
	stripped := trailingCommaRe.ReplaceAll([]byte(validInput), []byte("$1"))
	var out map[string]interface{}
	if err := json.Unmarshal(stripped, &out); err != nil {
		t.Errorf("trailingCommaRe: structural trailing comma not correctly stripped: %v", err)
	}
}
