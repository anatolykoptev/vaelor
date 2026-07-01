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

// TestTSXHandler_SymbolLanguageAgreesWithDetectLanguageFromPath is the
// plan's Phase 0b fitness function: for EVERY extension the tsxHandler
// serves, every emitted Symbol.Language must equal DetectLanguageFromPath
// of that extension — the parser must never disagree with its own
// path-based detector.
func TestTSXHandler_SymbolLanguageAgreesWithDetectLanguageFromPath(t *testing.T) {
	src := []byte(`
function Greeter() {
	return <div>hi</div>;
}
`)
	for _, ext := range tsxLang.Extensions() {
		t.Run(ext, func(t *testing.T) {
			path := "component" + ext
			want := DetectLanguageFromPath(path)
			if want == "" {
				t.Fatalf("DetectLanguageFromPath(%q) = \"\" — no language detected", path)
			}
			result, err := ParseFile(path, src, ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", path, err)
			}
			if len(result.Symbols) == 0 {
				t.Fatalf("ParseFile(%q): expected at least one symbol, got none", path)
			}
			for _, sym := range result.Symbols {
				if sym.Language != want {
					t.Errorf("ParseFile(%q): symbol %q Language = %q, want %q (DetectLanguageFromPath)", path, sym.Name, sym.Language, want)
				}
			}
		})
	}
}
