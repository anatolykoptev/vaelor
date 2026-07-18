package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestParseSvelteSimpleInstance(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "simple_instance.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("simple_instance.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	var greet *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "greet" {
			greet = s
			break
		}
	}
	if greet == nil {
		t.Fatalf("no symbol named 'greet'; symbols: %v", svelteSymbolNames(result.Symbols))
	}
	if greet.Kind != parser.KindFunction {
		t.Errorf("greet.Kind = %q, want function", greet.Kind)
	}
	// greet is on line 2 of the original .svelte file (1-indexed)
	if greet.StartLine != 2 {
		t.Errorf("greet.StartLine = %d, want 2", greet.StartLine)
	}
}

func TestParseSvelteModuleSvelte4(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "module_svelte4.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("module_svelte4.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Both <script> blocks (module + instance) are extracted and parsed without error.
	// Plain `export const` / `export let` variable declarations are not captured by
	// the TypeScript query (which only captures arrow-function consts), so we only
	// verify language is correct and no error occurred.
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}
}

func TestParseSvelteModuleSvelte5(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "module_svelte5.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("module_svelte5.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	if !svelteContainsName(svelteSymbolNames(result.Symbols), "helper") {
		t.Errorf("missing helper symbol; got %v", svelteSymbolNames(result.Symbols))
	}
}

func TestParseSvelteLangTs(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "lang_ts.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("lang_ts.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	if !svelteContainsName(svelteSymbolNames(result.Symbols), "fetchUser") {
		t.Errorf("missing fetchUser symbol; got %v", svelteSymbolNames(result.Symbols))
	}
}

// TestParseSvelteTemplateRefs verifies that the Svelte handler populates
// ParseResult.TemplateRefs with capitalised component tags from the markup,
// mirroring the Astro handler (TestParseAstroTemplateRefs). This is the Phase-2
// Svelte-composition wire-up: <Card/> in the markup becomes a TemplateRef;
// lowercase HTML tags and <svelte:*> special elements do not.
func TestParseSvelteTemplateRefs(t *testing.T) {
	t.Parallel()
	src := []byte("<script>\n  import Card from './Card.svelte';\n</script>\n" +
		"<main>\n  <svelte:head><title>t</title></svelte:head>\n  <Card />\n</main>\n")
	result, err := parser.ParseFile("Home.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	names := make([]string, len(result.TemplateRefs))
	for i, r := range result.TemplateRefs {
		names[i] = r.Name
	}
	if !svelteContainsName(names, "Card") {
		t.Errorf("TemplateRefs missing Card; got %v", names)
	}
	for _, n := range names {
		if n == "main" || n == "title" || n == "svelte:head" {
			t.Errorf("TemplateRefs contains non-component tag %q", n)
		}
	}
}

func svelteSymbolNames(syms []*parser.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name)
	}
	return names
}

func svelteContainsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
