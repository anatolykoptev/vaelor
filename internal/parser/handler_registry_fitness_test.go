package parser

import "testing"

// Registry-wide fitness functions (frontend-parse-parity Phase 5, deferred
// from Phases 0b and 1 — see docs/adr/0001-frontend-parse-parity.md). Both
// tests below range the LIVE registry map (registry, handler.go:73) — never a
// hand-maintained handler list — so a future handler is covered automatically
// the moment its init() calls registerHandler.

// ── deliverable #2: Symbol.Language agrees with DetectLanguageFromPath ─

// TestRegistryWideSymbolLanguageAgreesWithDetector generalizes
// TestJSTSFamily_SymbolLanguageAgreesWithDetector (handler_tsx_test.go),
// which proved the invariant only for the two handlers Phase 0b (PR #268)
// happened to fix — tsxLang (.tsx/.jsx) and tsLang (.ts/.js/.mjs/.cjs/.cts/
// .mts), both routed through the shared applyDetectedSymbolLanguage helper. A
// council LOW finding on that PR noted the opt-in-by-convention risk: a
// FUTURE handler that serves more than one canonical language through one
// shared grammar (the exact shape that produced the Phase 0b mislabel) could
// reintroduce the class without any existing test noticing, since the JS/TS
// test only ranges its own two handlers' Extensions().
//
// This test closes that gap by walking EVERY handler in the registry and
// asserting, for every extension each declares via Extensions(), that a
// parsed Symbol.Language equals DetectLanguageFromPath(ext). It reuses
// registrationFixtures (handler_registration_test.go) — the same
// per-extension fixture corpus TestHandlerRegistrationHealth already proves
// yields >=1 symbol — instead of hand-rolling a second fixture set.
func TestRegistryWideSymbolLanguageAgreesWithDetector(t *testing.T) {
	fixtures := registrationFixtures(t)

	for ext := range registry {
		t.Run(ext, func(t *testing.T) {
			src, ok := fixtures[ext]
			if !ok {
				t.Fatalf("no fixture for registered extension %q — add one in registrationFixtures", ext)
			}

			path := "lang_fitness" + ext
			want := DetectLanguageFromPath(path)
			if want == "" {
				t.Fatalf("DetectLanguageFromPath(%q) = \"\" for a registered extension — DetectLanguageFromPath and the registry have drifted", path)
			}

			result, err := ParseFile(path, src, ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): %v", path, err)
			}
			if len(result.Symbols) == 0 {
				// Fixture-yields-a-symbol is TestHandlerRegistrationHealth's
				// concern, not this test's; skip with a logged reason rather
				// than vacuously ranging zero symbols and reporting green.
				t.Skipf("ParseFile(%q) produced 0 symbols — nothing to check Language on for this extension; see TestHandlerRegistrationHealth for fixture health", path)
			}
			for _, sym := range result.Symbols {
				if sym.Language != want {
					t.Errorf("ParseFile(%q): symbol %q Language = %q, want %q (DetectLanguageFromPath)", path, sym.Name, sym.Language, want)
				}
			}
		})
	}
}

// ── deliverable #3: scriptCallSource implies markupCallSource ──────────

// TestScriptCallSourceImpliesMarkupCallSource guards the ExtractCalls
// single-producer split (calls.go): a handler implementing scriptCallSource
// gets its calls routed through ScriptCalls INSTEAD OF the raw whole-file
// CallsQuery fallback (see the scriptCallSource doc comment) — that raw
// fallback is deliberately skipped for these handlers because running it
// unmodified would double-emit garbled template calls (the duplicate-edge
// class TestNoDuplicateMarkupEdges in markup_parity_test.go regression-guards).
//
// That means a handler implementing scriptCallSource WITHOUT ALSO
// implementing markupCallSource would end up with NO producer at all for its
// template region: not the raw fallback (skipped on purpose) and not
// MarkupCalls (not implemented) — every template-region call silently
// vanishes. astroHandler and svelteHandler both implement the full pair today
// (markup_calls.go); this test forecloses a future third preprocessor
// handler (e.g. a Vue template pass) shipping only half of it.
func TestScriptCallSourceImpliesMarkupCallSource(t *testing.T) {
	for ext, h := range registry {
		_, hasScript := h.(scriptCallSource)
		_, hasMarkup := h.(markupCallSource)
		if hasScript && !hasMarkup {
			t.Errorf("%s: handler %T implements scriptCallSource but not markupCallSource — ExtractCalls routes its calls through ScriptCalls INSTEAD OF the raw whole-file CallsQuery fallback, so every template-region call would be silently dropped (calls.go, ExtractCalls)", ext, h)
		}
	}
}
