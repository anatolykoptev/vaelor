package parser_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// fixtureForExt maps every registered file extension to a representative fixture
// whose content is valid for that extension's handler. Extensions that share a
// handler (e.g. .ts/.js/.mjs, .cpp/.cc/.hpp) reuse one fixture — the handler code is
// identical, and TestParseFileWithCalls_EquivalentToSeparate still exercises each
// extension's own language labeling via the sample<ext> path it parses under.
//
// TestParseFileWithCalls_EquivalentToSeparate FAILS if any parser.RegisteredExtensions()
// entry is missing here, so a newly registered handler cannot ship without an
// equivalence fixture proving ParseFileWithCalls == ParseFile + ExtractCalls.
var fixtureForExt = map[string]string{
	".go":     filepath.Join("testdata", "sample.go"),
	".py":     filepath.Join("testdata", "sample.py"),
	".rs":     filepath.Join("testdata", "sample.rs"),
	".java":   filepath.Join("testdata", "sample.java"),
	".cs":     filepath.Join("testdata", "sample.cs"),
	".rb":     filepath.Join("testdata", "sample.rb"),
	".php":    filepath.Join("testdata", "sample.php"),
	".c":      filepath.Join("testdata", "sample.c"),
	".h":      filepath.Join("testdata", "sample.c"),
	".cpp":    filepath.Join("testdata", "sample.cpp"),
	".cc":     filepath.Join("testdata", "sample.cpp"),
	".cxx":    filepath.Join("testdata", "sample.cpp"),
	".hpp":    filepath.Join("testdata", "sample.cpp"),
	".ts":     filepath.Join("testdata", "sample.ts"),
	".js":     filepath.Join("testdata", "sample.ts"),
	".mjs":    filepath.Join("testdata", "sample.ts"),
	".cjs":    filepath.Join("testdata", "sample.ts"),
	".cts":    filepath.Join("testdata", "sample.ts"),
	".mts":    filepath.Join("testdata", "sample.ts"),
	".tsx":    filepath.Join("testdata", "sample.tsx"),
	".jsx":    filepath.Join("testdata", "sample.tsx"),
	".kt":     filepath.Join("testdata", "sample.kt"),
	".kts":    filepath.Join("testdata", "sample.kt"),
	".swift":  filepath.Join("testdata", "sample.swift"),
	".html":   filepath.Join("testdata", "sample.html"),
	".gohtml": filepath.Join("testdata", "sample.html"),
	".tmpl":   filepath.Join("testdata", "sample.html"),
	".svelte": filepath.Join("testdata", "svelte", "runes_basic.svelte"),
	".astro":  filepath.Join("testdata", "astro", "template_refs.astro"),
	".vue":    filepath.Join("testdata", "vue", "script_setup_ts.vue"),
}

// equivOpts are the ParseOpts variants the equivalence proof runs under, mirroring
// the two real caller shapes: the heavy index path (body+imports+type-rels) and the
// lightweight explore/focus path (imports only). Language is left empty so the
// detection + applyDetectedSymbolLanguage relabel branch (.js->javascript,
// .jsx->javascript) is exercised on both the shared and fallback paths.
var equivOpts = []parser.ParseOpts{
	{IncludeBody: true, IncludeImports: true, IncludeTypeRels: true},
	{IncludeImports: true},
}

// TestParseFileWithCalls_EquivalentToSeparate is the correctness gate for issue #400:
// for every registered language, the combined single-parse ParseFileWithCalls returns
// a ParseResult DEEP-EQUAL to ParseFile and calls DEEP-EQUAL to ExtractCalls. This
// proves the single shared parse is byte-identical to the historical double parse,
// across both the shared-tree path (Go/TS/Python/... + typescript/tsx) and the
// fallback path (svelte/astro/vue/html).
func TestParseFileWithCalls_EquivalentToSeparate(t *testing.T) {
	for _, ext := range parser.RegisteredExtensions() {
		fixture, ok := fixtureForExt[ext]
		if !ok {
			t.Errorf("registered extension %q has no equivalence fixture — add one to fixtureForExt", ext)
			continue
		}

		src := readFixture(t, fixture)
		path := "sample" + ext

		for _, opts := range equivOpts {
			assertEquivalent(t, path, src, opts)
		}
	}
}

// TestParseFileWithCalls_MalformedEquivalent proves equivalence holds on
// error-recovery trees too — malformed source, where tree-sitter emits ERROR nodes
// and call/symbol extraction has historically garbled. Covers the shared path (Go,
// TS, Python) and the fallback path (Svelte).
func TestParseFileWithCalls_MalformedEquivalent(t *testing.T) {
	broken := map[string]string{
		"bad.go":     "package p\n\nfunc Broken( {\n\treturn helper(\n}\n",
		"bad.ts":     "export function broken( {\n  return helper(\n}\n",
		"bad.py":     "def broken(:\n    return helper(\n",
		"bad.svelte": "<script>\nfunction broken( {\n  return helper(\n}\n</script>\n<p>{missing(</p>\n",
	}
	for path, code := range broken {
		for _, opts := range equivOpts {
			assertEquivalent(t, path, []byte(code), opts)
		}
	}
}

// TestExtractCallsOptsInvariant proves the analyze caller's opts unification is safe:
// call extraction ignores every ParseOpts field beyond Language, so ExtractCalls with
// the heavy index opts returns calls identical to ExtractCalls with Language-only opts
// (analyze formerly passed the latter to ExtractCalls, the former to ParseFile).
func TestExtractCallsOptsInvariant(t *testing.T) {
	for _, ext := range parser.RegisteredExtensions() {
		fixture := fixtureForExt[ext]
		if fixture == "" {
			continue
		}
		src := readFixture(t, fixture)
		path := "sample" + ext

		heavy, err := parser.ExtractCalls(path, src, parser.ParseOpts{IncludeBody: true, IncludeImports: true, IncludeTypeRels: true})
		if err != nil {
			t.Fatalf("ExtractCalls(heavy, %s): %v", path, err)
		}
		langOnly, err := parser.ExtractCalls(path, src, parser.ParseOpts{})
		if err != nil {
			t.Fatalf("ExtractCalls(langOnly, %s): %v", path, err)
		}
		if !reflect.DeepEqual(heavy, langOnly) {
			t.Errorf("%s: ExtractCalls depends on ParseOpts — analyze opts unification is unsafe\n heavy=%#v\n langOnly=%#v", path, heavy, langOnly)
		}
	}
}

// assertEquivalent fails unless ParseFileWithCalls(path, src, opts) returns a result
// and calls deep-equal to the separate ParseFile + ExtractCalls pair.
func assertEquivalent(t *testing.T, path string, src []byte, opts parser.ParseOpts) {
	t.Helper()

	wantPR, wantErr := parser.ParseFile(path, src, opts)
	wantCalls, wantCallErr := parser.ExtractCalls(path, src, opts)
	gotPR, gotCalls, gotErr := parser.ParseFileWithCalls(path, src, opts)

	if (wantErr == nil) != (gotErr == nil) {
		t.Errorf("%s (opts %+v): error mismatch: ParseFile err=%v, ParseFileWithCalls err=%v", path, opts, wantErr, gotErr)
		return
	}
	if wantErr != nil {
		return // both errored — nothing more to compare
	}
	if wantCallErr != nil {
		t.Fatalf("%s: ExtractCalls returned error: %v", path, wantCallErr)
	}

	if !reflect.DeepEqual(wantPR, gotPR) {
		t.Errorf("%s (opts %+v): ParseResult diverges from ParseFile\n want=%#v\n got=%#v", path, opts, wantPR, gotPR)
	}
	if !reflect.DeepEqual(wantCalls, gotCalls) {
		t.Errorf("%s (opts %+v): calls diverge from ExtractCalls\n want=%#v\n got=%#v", path, opts, wantCalls, gotCalls)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return src
}
