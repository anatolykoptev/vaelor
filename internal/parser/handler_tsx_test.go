package parser

import "testing"

// TestTSXHandler_JSXSymbolLanguage guards ADR 5 (plans/go-code/2026-06-30-
// frontend-parse-parity-react-svelte-astro.md): tsxHandler serves BOTH .tsx
// and .jsx through one shared handler (handler_tsx.go), and its MapCapture
// delegates to tsLang.MapCapture (handler_typescript.go), which hardcodes
// Symbol.Language = "typescript" on every emitted symbol regardless of the
// actual file extension. DetectLanguageFromPath already correctly maps
// .jsx -> "javascript" (matching GitHub Linguist) — this test asserts the
// parser AGREES with its own path-based detector for .jsx symbols.
func TestTSXHandler_JSXSymbolLanguage(t *testing.T) {
	t.Parallel()
	src := []byte(`
function Greeter() {
	return <div>hi</div>;
}

class Widget {
	render() {
		return <span>ok</span>;
	}
}
`)
	result, err := ParseFile("component.jsx", src, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(result.Symbols) == 0 {
		t.Fatal("expected at least one symbol, got none")
	}
	for _, sym := range result.Symbols {
		if sym.Language != "javascript" {
			t.Errorf("symbol %q: Language = %q, want %q (DetectLanguageFromPath(.jsx))", sym.Name, sym.Language, "javascript")
		}
	}
}

// TestTSXHandler_TSXSymbolLanguageUnchanged guards the boundaries-HIGH trap
// the plan explicitly calls out: fixing .jsx by flipping the SHARED
// handler's lang field (or tsLang.MapCapture's literal) to "javascript"
// would mislabel EVERY .tsx symbol too — a worse fleet-wide regression.
// .tsx must keep emitting Symbol.Language == "typescript".
func TestTSXHandler_TSXSymbolLanguageUnchanged(t *testing.T) {
	t.Parallel()
	src := []byte(`
function Greeter(): JSX.Element {
	return <div>hi</div>;
}

class Widget {
	render(): JSX.Element {
		return <span>ok</span>;
	}
}
`)
	result, err := ParseFile("component.tsx", src, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(result.Symbols) == 0 {
		t.Fatal("expected at least one symbol, got none")
	}
	for _, sym := range result.Symbols {
		if sym.Language != "typescript" {
			t.Errorf("symbol %q: Language = %q, want %q (DetectLanguageFromPath(.tsx))", sym.Name, sym.Language, "typescript")
		}
	}
}

// TestJSTSFamily_OptsLanguageOverrideHonored locks in override-first precedence
// (matching ParseFile, parser.go). The sparse-embedding backfill re-parses
// stored rows with ParseOpts{Language: storedRow.Language} so buildEmbedText
// reproduces the stored hash; the symbol-language correction MUST honor a
// non-empty opts.Language. Otherwise every pre-existing .jsx/.js row (indexed as
// "typescript" before parity) would re-parse as "javascript", its hash would
// change, and the backfill would leave it a NULL sparse vector that never heals.
func TestJSTSFamily_OptsLanguageOverrideHonored(t *testing.T) {
	t.Parallel()
	src := []byte("function greet() { return 1; }")
	for _, path := range []string{"component.jsx", "mod.js", "mod.mjs", "mod.cjs"} {
		t.Run(path, func(t *testing.T) {
			result, err := ParseFile(path, src, ParseOpts{Language: "typescript"})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", path, err)
			}
			if len(result.Symbols) == 0 {
				t.Fatalf("ParseFile(%q): no symbols", path)
			}
			for _, sym := range result.Symbols {
				if sym.Language != "typescript" {
					t.Errorf("ParseFile(%q, Language=typescript): symbol %q Language = %q, want %q (override-first must honor opts.Language for backfill hash reproduction)", path, sym.Name, sym.Language, "typescript")
				}
			}
		})
	}
}

// TestSvelteRuneModule_SymbolLanguageUniform guards the intra-file split caught
// in re-review: in a .svelte.js/.svelte.ts rune module, typescriptHandler.Parse
// appends KindRune symbols alongside the ordinary (function/class) symbols, and
// applyDetectedSymbolLanguage must relabel BOTH so every symbol in one file shares
// one Language == DetectLanguageFromPath(path). The regressive version corrected
// ordinary symbols to "javascript" for .svelte.js while the appended rune symbols
// kept the stale hardcoded "typescript" (result.Language) — an inconsistency that
// did not exist pre-parity (both were uniformly "typescript"). The fixture mixes an
// ordinary declaration with runes so the split is actually exercised.
func TestSvelteRuneModule_SymbolLanguageUniform(t *testing.T) {
	t.Parallel()
	src := []byte("export function makeCounter() { return 0; }\nexport const counter = $state(0);\nexport const doubled = $derived(counter * 2);\n")
	for _, path := range []string{"store.svelte.js", "store.svelte.ts"} {
		t.Run(path, func(t *testing.T) {
			want := DetectLanguageFromPath(path)
			result, err := ParseFile(path, src, ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", path, err)
			}
			var runeSeen, ordinarySeen bool
			for _, sym := range result.Symbols {
				if sym.Kind == KindRune {
					runeSeen = true
				} else {
					ordinarySeen = true
				}
				if sym.Language != want {
					t.Errorf("ParseFile(%q): symbol %q (kind=%q) Language = %q, want %q (ordinary + rune must be uniform)", path, sym.Name, sym.Kind, sym.Language, want)
				}
			}
			if !runeSeen {
				t.Fatalf("ParseFile(%q): expected at least one KindRune symbol, got none", path)
			}
			if !ordinarySeen {
				t.Fatalf("ParseFile(%q): expected at least one ordinary symbol so the intra-file split is exercised, got none", path)
			}
		})
	}
}
