package callgraph

import (
	"os"
	"path/filepath"
	"testing"

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
	// Flush alias cache to prevent cross-test pollution.
	aliasCache.Delete(testRoot)

	// Write a tsconfig.json into testRoot.
	// testRoot is /repo (from astro_resolve_test.go); we need a real temp dir here.
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
