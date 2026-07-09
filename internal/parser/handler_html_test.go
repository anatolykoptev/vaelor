package parser_test

// Tests for HTML handler (Wave 1: Go html/template preproc + {{define}} symbol extraction).
//
// Anti-tautology contract: every test imports parser.ParseFile and asserts on
// the returned *parser.ParseResult — no local vars, no structural impossibility.
// See feedback_tdd_implementer_tautology_tests.md for the recurring failure class.

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestParseHTMLFile_templateDefine verifies that a single {{define "X"}} block
// produces a symbol Name="hunt_jobs", Kind=KindFunction in a .html file.
func TestParseHTMLFile_templateDefine(t *testing.T) {
	t.Parallel()
	src := []byte(`{{define "hunt_jobs"}}
<div class="page-header">
  <h1>Jobs</h1>
</div>
{{end}}
`)
	result, err := parser.ParseFile("hunt_jobs.html", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "html" {
		t.Errorf("Language = %q, want %q", result.Language, "html")
	}

	// Find the "hunt_jobs" symbol.
	var sym *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "hunt_jobs" {
			sym = s
			break
		}
	}
	if sym == nil {
		t.Fatalf("symbol %q not found; all symbols: %v", "hunt_jobs", htmlSymbolNames(result.Symbols))
	}
	if sym.Kind != parser.KindFunction {
		t.Errorf("sym.Kind = %q, want %q", sym.Kind, parser.KindFunction)
	}
	// EndLine must be > StartLine (the block spans multiple lines).
	if sym.EndLine <= sym.StartLine {
		t.Errorf("EndLine(%d) must be > StartLine(%d)", sym.EndLine, sym.StartLine)
	}
}

// TestParseHTMLFile_multipleTemplates verifies that two {{define}} blocks in one
// file both produce symbols.
func TestParseHTMLFile_multipleTemplates(t *testing.T) {
	t.Parallel()
	src := []byte(`{{define "layout"}}<html><body>{{template "content" .}}</body></html>{{end}}
{{define "content"}}<h1>Page</h1>{{end}}
`)
	result, err := parser.ParseFile("layout.gohtml", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "html" {
		t.Errorf("Language = %q, want %q", result.Language, "html")
	}

	byName := make(map[string]*parser.Symbol)
	for _, s := range result.Symbols {
		byName[s.Name] = s
	}

	for _, want := range []string{"layout", "content"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("symbol %q not found; all symbols: %v", want, htmlSymbolNames(result.Symbols))
		}
	}
}

// TestParseHTMLFile_pureHTML verifies that a plain HTML file (no Go template
// actions) parses without panic and has Language="html" with no template symbols.
func TestParseHTMLFile_pureHTML(t *testing.T) {
	t.Parallel()
	src := []byte(`<!DOCTYPE html>
<html>
  <body>
    <h1>Just HTML</h1>
  </body>
</html>
`)
	result, err := parser.ParseFile("static.html", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "html" {
		t.Errorf("Language = %q, want %q", result.Language, "html")
	}
	// No {{define}} blocks — should have no symbols.
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for pure HTML, got %v", htmlSymbolNames(result.Symbols))
	}
}

// TestParseHTMLFile_extensionRouting verifies that .html, .gohtml, and .tmpl
// all route to Language="html".
func TestParseHTMLFile_extensionRouting(t *testing.T) {
	t.Parallel()
	src := []byte(`<!DOCTYPE html><html><body></body></html>`)

	cases := []struct {
		path string
	}{
		{"page.html"},
		{"layout.gohtml"},
		{"admin.tmpl"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			result, err := parser.ParseFile(tc.path, src, parser.ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", tc.path, err)
			}
			if result.Language != "html" {
				t.Errorf("ParseFile(%q).Language = %q, want %q", tc.path, result.Language, "html")
			}
		})
	}
}

// htmlSymbolNames is a local helper (does not conflict with astroSymbolNames
// in handler_astro_test.go — both are in package parser_test but in different
// files; they are distinct identifiers).
func htmlSymbolNames(syms []*parser.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name)
	}
	return names
}
