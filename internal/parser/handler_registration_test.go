package parser

import (
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

// ── registerHandler collision guard ─────────────────────────────────

// fakeHandler is a minimal LanguageHandler stub used only to exercise
// registerHandler in isolation from any real tree-sitter grammar.
type fakeHandler struct {
	lang string
	exts []string
}

func (f *fakeHandler) Language() string     { return f.lang }
func (f *fakeHandler) Extensions() []string { return f.exts }

func (f *fakeHandler) Parse(path string, _ []byte, _ ParseOpts) (*ParseResult, error) {
	return &ParseResult{File: path, Language: f.lang}, nil
}

func (f *fakeHandler) Capabilities() Capabilities { return Capabilities{} }

func (f *fakeHandler) MapCapture(_ string, _ *sitter.Node, _ []byte) *Symbol { return nil }

// TestRegisterHandlerCollisionPanics asserts the single-owner-per-extension
// invariant (plan ADR 8, plans/go-code/2026-06-30-frontend-parse-parity-
// react-svelte-astro.md): registerHandler must panic when an extension is
// already claimed by another handler, rather than silently overwriting the
// registry entry (the pre-fix behavior — a plain `registry[ext] = h`). This
// forecloses any future grammar handler (e.g. a native tree-sitter-svelte
// driver, plan Phase 4) silently stealing an already-registered extension
// like ".svelte" without anyone noticing until symbols/edges silently
// changed producer.
func TestRegisterHandlerCollisionPanics(t *testing.T) {
	t.Parallel()
	const testExt = ".gocode_test_synthetic"
	t.Cleanup(func() { delete(registry, testExt) })

	registerHandler(&fakeHandler{lang: "synthetic-a", exts: []string{testExt}})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("registerHandler: expected panic on double-registration of " + testExt + ", got none")
		}
	}()
	registerHandler(&fakeHandler{lang: "synthetic-b", exts: []string{testExt}})
	t.Fatal("registerHandler: unreachable — should have panicked before returning")
}

// ── registration health: Capabilities + minimal parse per extension ────

// registrationFixtures maps every extension in the live handler registry to
// a minimal, handler-appropriate source snippet containing at least one
// symbol. Sibling extensions served by the same handler (e.g. .c/.h,
// .cpp/.cc/.cxx/.hpp, .ts/.js/.mjs/.cjs/.cts/.mts, .tsx/.jsx, .kt/.kts,
// .html/.gohtml/.tmpl) intentionally reuse the same bytes — the handler
// doesn't change parsing behavior by extension, only Language()/dispatch.
// Fixtures for languages that already have a testdata/sample.* file reuse it
// (proven-working, avoids re-deriving syntax); the rest are small inline
// snippets mirroring the patterns already used in parser_languages_test.go /
// calls_test.go.
func registrationFixtures(t *testing.T) map[string][]byte {
	t.Helper()

	read := func(rel ...string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, rel...)...))
		if err != nil {
			t.Fatalf("read fixture %v: %v", rel, err)
		}
		return b
	}

	goSrc := read("sample.go")
	pySrc := read("sample.py")
	rsSrc := read("sample.rs")
	javaSrc := read("sample.java")
	phpSrc := read("sample.php")
	rbSrc := read("sample.rb")
	cSrc := read("sample.c")
	cppSrc := read("sample.cpp")
	csSrc := read("sample.cs")
	tsSrc := read("sample.ts")
	astroSrc := read("astro", "frontmatter_only.astro")
	svelteSrc := read("svelte", "simple_instance.svelte")
	vueSrc := read("vue", "script_setup_ts.vue")

	kotlinSrc := []byte("fun sayHi(): String {\n\treturn \"hi\"\n}")
	swiftSrc := []byte("func sayHi() -> String {\n\treturn \"hi\"\n}")
	tsxSrc := []byte("function SayHi() {\n\treturn <div>hi</div>;\n}\n")
	htmlSrc := []byte(`{{define "sayHi"}}hi{{end}}`)

	return map[string][]byte{
		".go":     goSrc,
		".py":     pySrc,
		".rs":     rsSrc,
		".java":   javaSrc,
		".php":    phpSrc,
		".rb":     rbSrc,
		".c":      cSrc,
		".h":      cSrc,
		".cpp":    cppSrc,
		".cc":     cppSrc,
		".cxx":    cppSrc,
		".hpp":    cppSrc,
		".cs":     csSrc,
		".ts":     tsSrc,
		".js":     tsSrc,
		".mjs":    tsSrc,
		".cjs":    tsSrc,
		".cts":    tsSrc,
		".mts":    tsSrc,
		".astro":  astroSrc,
		".svelte": svelteSrc,
		".vue":    vueSrc,
		".kt":     kotlinSrc,
		".kts":    kotlinSrc,
		".swift":  swiftSrc,
		".tsx":    tsxSrc,
		".jsx":    tsxSrc,
		".html":   htmlSrc,
		".gohtml": htmlSrc,
		".tmpl":   htmlSrc,
	}
}

// isGrammarBacked reports whether ext's handler is expected to expose a real
// tree-sitter grammar via Capabilities(). The html family legitimately has
// none — it uses the preproc/astro_refs.go-style byte-walker instead (see
// handler_html.go) — so it is excluded from the Capabilities nil-checks
// below; every other registered extension goes through a tree-sitter grammar,
// either its own or (for astro/svelte/vue/tsx) one borrowed lazily from
// tsLang.
func isGrammarBacked(ext string) bool {
	switch ext {
	case ".html", ".gohtml", ".tmpl":
		return false
	default:
		return true
	}
}

// TestHandlerRegistrationHealth walks every extension actually registered in
// the live handler registry and proves, for each one, that (1) Capabilities()
// resolves a real grammar/query set — not a zero-value struct frozen by an
// init-order bug — and (2) ParseFile succeeds through the real ParseFile ->
// HandlerForExt -> handler.Parse entry point on a minimal source file,
// yielding at least one symbol.
//
// Regression guard for the MapCapture pointer-receiver init-order landmine
// documented at commit a614d73 ("mirror init-order note from handler_svelte
// to handler_astro"): astro/svelte/vue/tsx all borrow tsLang's Capabilities
// lazily at call time (see the doc comments on handler_astro.go /
// handler_svelte.go / handler_vue.go) because Go runs init() in alphabetical
// file-name order — handler_astro.go < handler_typescript.go — so an eager
// copy of tsLang.Capabilities() taken AT init() time would freeze a
// zero-value Capabilities{} (nil SitterLanguage/TagsQuery/MapCapture) into
// the handler forever.
//
// That class of bug would NOT be caught by the existing per-fixture parse
// tests alone: astro/svelte/vue's Parse() goes through parseWithTSAndRemap,
// which reads tsLang directly and bypasses the handler's own Capabilities()
// method entirely, so Parse() can keep working even if Capabilities() is
// broken. This test additionally calls Capabilities() itself, matching how
// external callers like ExtractCalls / ExtractRelationships decide whether
// call/relationship extraction is supported for a given file extension —
// closing that blind spot for every registered handler, not just the four
// named in the landmine's history.
func TestHandlerRegistrationHealth(t *testing.T) {
	t.Parallel()
	fixtures := registrationFixtures(t)

	for ext := range registry {
		t.Run(ext, func(t *testing.T) {
			h := HandlerForExt(ext)
			if h == nil {
				t.Fatalf("HandlerForExt(%q) = nil, want a registered handler", ext)
			}

			if isGrammarBacked(ext) {
				caps := h.Capabilities()
				if caps.SitterLanguage == nil {
					t.Errorf("%s: Capabilities().SitterLanguage is nil (init-order landmine?)", ext)
				}
				if caps.TagsQuery == nil {
					t.Errorf("%s: Capabilities().TagsQuery is nil (init-order landmine?)", ext)
				}
				if caps.MapCapture == nil {
					t.Errorf("%s: Capabilities().MapCapture is nil (init-order landmine?)", ext)
				}
			}

			src, ok := fixtures[ext]
			if !ok {
				t.Fatalf("no registration-health fixture defined for extension %q — add one in registrationFixtures", ext)
			}

			result, err := ParseFile("registration_health"+ext, src, ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", ext, err)
			}
			if len(result.Symbols) == 0 {
				t.Errorf("%s: ParseFile produced 0 symbols for a fixture that defines one — Capabilities/MapCapture likely broken", ext)
			}
		})
	}
}
