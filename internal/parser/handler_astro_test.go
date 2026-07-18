package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestParseAstroFrontmatterOnly(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "frontmatter_only.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("frontmatter_only.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "astro" {
		t.Errorf("Language = %q, want astro", result.Language)
	}

	var sayHi *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "sayHi" {
			sayHi = s
			break
		}
	}
	if sayHi == nil {
		t.Fatalf("no symbol named 'sayHi'; symbols: %v", astroSymbolNames(result.Symbols))
	}
	if sayHi.Kind != parser.KindFunction {
		t.Errorf("sayHi.Kind = %q, want function", sayHi.Kind)
	}
	// sayHi is on line 5 of the original .astro file (1-indexed)
	if sayHi.StartLine != 5 {
		t.Errorf("sayHi.StartLine = %d, want 5", sayHi.StartLine)
	}
}

func TestParseAstroScriptOnly(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "script_only.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("script_only.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "astro" {
		t.Errorf("Language = %q, want astro", result.Language)
	}
	if !astroContainsName(astroSymbolNames(result.Symbols), "init") {
		t.Errorf("missing init symbol; got %v", astroSymbolNames(result.Symbols))
	}
}

func TestParseAstroFrontmatterAndScript(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "frontmatter_and_script.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("frontmatter_and_script.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "astro" {
		t.Errorf("Language = %q, want astro", result.Language)
	}

	var track *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "track" {
			track = s
			break
		}
	}
	if track == nil {
		t.Fatalf("no symbol named 'track'; symbols: %v", astroSymbolNames(result.Symbols))
	}
	// track is remapped to original line 5 — the preproc maps the first virtual
	// line of the <script> block to the line containing contentStart (the '<script>'
	// tag line itself, line 5). The leading '\n' inside the block advances origLine
	// to 6 on the next virtual line, but tree-sitter places the function node on
	// virtual line 4 which maps back to original line 5.
	if track.StartLine != 5 {
		t.Errorf("track.StartLine = %d, want 5", track.StartLine)
	}
}

func TestParseAstroEmpty(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "empty.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("empty.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "astro" {
		t.Errorf("Language = %q, want astro", result.Language)
	}
	// No TypeScript/JS content — symbol list should be empty or very small.
	if len(result.Symbols) > 0 {
		t.Errorf("expected no symbols for HTML-only file, got %v", astroSymbolNames(result.Symbols))
	}
}

func astroSymbolNames(syms []*parser.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name)
	}
	return names
}

func astroContainsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

func TestParseAstroTemplateRefs(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "template_refs.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("template_refs.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "astro" {
		t.Errorf("Language = %q, want astro", result.Language)
	}

	names := make([]string, len(result.TemplateRefs))
	for i, r := range result.TemplateRefs {
		names[i] = r.Name
	}

	wantNames := []string{"Header", "Breadcrumbs"}
	for _, want := range wantNames {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TemplateRefs missing %q; got %v", want, names)
		}
	}

	// div, main, slot are lowercase — must not appear
	for _, n := range names {
		if n == "div" || n == "main" || n == "slot" {
			t.Errorf("TemplateRefs contains HTML tag %q", n)
		}
	}
}

func TestParseAstroNoTemplateRefs(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "frontmatter_only.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("frontmatter_only.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// frontmatter_only has no capitalised JSX tags in the template body
	for _, r := range result.TemplateRefs {
		t.Errorf("unexpected TemplateRef %q in frontmatter-only file", r.Name)
	}
}
