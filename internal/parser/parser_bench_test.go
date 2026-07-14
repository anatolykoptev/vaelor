package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// benchFixture pairs a benchmark sub-test name with its fixture path
// (relative to testdata/), one per supported language that has a fixture
// under internal/parser/testdata.
type benchFixture struct {
	name string
	path string
}

// benchFixtures lists one representative fixture per language. Reuses the
// same sample.* / svelte/ / astro/ / vue/ fixtures the parser_languages_test.go
// and handler_*_test.go suites already load via os.ReadFile(filepath.Join("testdata", ...)).
var benchFixtures = []benchFixture{
	{"Go", filepath.Join("testdata", "sample.go")},
	{"TypeScript", filepath.Join("testdata", "sample.ts")},
	{"Python", filepath.Join("testdata", "sample.py")},
	{"Rust", filepath.Join("testdata", "sample.rs")},
	{"Java", filepath.Join("testdata", "sample.java")},
	{"C", filepath.Join("testdata", "sample.c")},
	{"Cpp", filepath.Join("testdata", "sample.cpp")},
	{"CSharp", filepath.Join("testdata", "sample.cs")},
	{"Ruby", filepath.Join("testdata", "sample.rb")},
	{"PHP", filepath.Join("testdata", "sample.php")},
	{"Svelte", filepath.Join("testdata", "svelte", "runes_basic.svelte")},
	{"Astro", filepath.Join("testdata", "astro", "template_refs.astro")},
	{"Vue", filepath.Join("testdata", "vue", "script_setup_ts.vue")},
}

// BenchmarkParseFile benchmarks parser.ParseFile — the single production
// entrypoint every caller (go-code CLI, MCP tools, compare.BuildSnapshot)
// goes through — for one fixture per supported language. ParseOpts mirrors
// compare.parseSnapshotFile's call shape (internal/compare/snapshot.go),
// the heaviest realistic caller: IncludeBody + IncludeImports + IncludeTypeRels
// all on. This is a prerequisite for #399 parser-perf work: establishes the
// ns/op + allocs/op baseline per language before any optimization lands.
//
// ParseFile does not accept an injectable *sitter.Parser via ParseOpts — each
// call builds its own tree-sitter parser internally (parseTree in
// internal/parser/parser_base.go), so there is nothing to hoist out of the
// b.N loop besides the fixture bytes themselves.
func BenchmarkParseFile(b *testing.B) {
	opts := parser.ParseOpts{
		IncludeBody:     true,
		IncludeImports:  true,
		IncludeTypeRels: true,
	}

	for _, fx := range benchFixtures {
		source, err := os.ReadFile(fx.path)
		if err != nil {
			b.Fatalf("read %s: %v", fx.path, err)
		}

		b.Run(fx.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := parser.ParseFile(fx.path, source, opts); err != nil {
					b.Fatalf("ParseFile(%s): %v", fx.path, err)
				}
			}
		})
	}
}

// BenchmarkParseFileWithCalls benchmarks the combined symbols+calls path per
// language — the exact path issue #400 changes. On this branch it calls
// parser.ParseFileWithCalls (ONE shared tree-sitter parse); on origin/main the
// same-named benchmark calls parser.ParseFile + parser.ExtractCalls (the historical
// TWO parses of identical bytes). benchstat old.txt new.txt over the two checkouts
// therefore measures the double->single parse win directly. Opts mirror the heaviest
// caller (codegraph indexParseFile): body + imports + type-rels all on, which is also
// the shape ExtractCalls callers pair with ParseFile.
func BenchmarkParseFileWithCalls(b *testing.B) {
	opts := parser.ParseOpts{
		IncludeBody:     true,
		IncludeImports:  true,
		IncludeTypeRels: true,
	}

	for _, fx := range benchFixtures {
		source, err := os.ReadFile(fx.path)
		if err != nil {
			b.Fatalf("read %s: %v", fx.path, err)
		}

		b.Run(fx.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := parser.ParseFileWithCalls(fx.path, source, opts); err != nil {
					b.Fatalf("ParseFileWithCalls(%s): %v", fx.path, err)
				}
			}
		})
	}
}
