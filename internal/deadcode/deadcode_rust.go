package deadcode

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// rustWellKnownMethods are Rust trait method names that are called implicitly
// by the runtime or standard library and should never be flagged as dead code.
var rustWellKnownMethods = map[string]bool{
	"fmt": true, "clone": true, "drop": true, "default": true,
	"from": true, "into": true, "try_from": true, "try_into": true,
	"as_ref": true, "as_mut": true, "deref": true, "deref_mut": true,
	"next": true, "into_iter": true, "to_string": true,
	"serialize": true, "deserialize": true, "poll": true,
	"source": true, "description": true,
	"eq": true, "ne": true, "partial_cmp": true, "cmp": true, "hash": true,
	"add": true, "sub": true, "mul": true, "div": true,
	"index": true, "index_mut": true,
	"borrow": true, "borrow_mut": true,
}

// hasTestAttribute checks if a symbol has a test-related attribute (Rust #[test], #[tokio::test]).
func hasTestAttribute(sym *parser.Symbol) bool {
	for _, attr := range sym.Attributes {
		if strings.Contains(attr, "test") {
			return true
		}
	}
	return false
}

// isRustWellKnownMethod checks if a Rust method name matches a well-known trait method.
func isRustWellKnownMethod(sym *parser.Symbol) bool {
	return sym.Language == "rust" && sym.Kind == parser.KindMethod && rustWellKnownMethods[sym.Name]
}

// isRustFrameworkHandler returns true for Rust functions that are likely framework
// entry points registered via Axum, Actix, or Rocket — not dead even without explicit callers.
func isRustFrameworkHandler(sym *parser.Symbol) bool {
	if sym.Language != "rust" {
		return false
	}
	name := sym.Name
	// Common Axum/Actix/Rocket entry-point names and naming conventions.
	return name == "handler" || name == "serve" || name == "run" ||
		strings.HasPrefix(name, "handle_") ||
		strings.HasSuffix(name, "_handler")
}

// isSymbolExported returns true if the symbol is public/exported.
//
// IsPublic always wins when the parser set it. Below that:
//
//   - Rust trusts IsPublic exclusively — never falls back to a name
//     heuristic. internal/parser/handler_rust.go's hasVisibilityModifier
//     reliably detects `pub` for every symbol kind it emits (function,
//     method, type, trait, const, static — see
//     TestRustVisibilityAndAttributes in internal/parser), so
//     IsPublic=false for a Rust symbol means genuinely private, not
//     "unknown." Dead-code detection needs the OPPOSITE bias from a
//     doc-coverage heuristic (langutil.IsExportedForDoc's "any
//     non-underscore name is exported" default): guessing "exported" here
//     would misclassify ordinary private snake_case helpers (fn helper())
//     as exported and SILENCE a real dead-code finding — see
//     TestAnalyze_RustPubVisibility and
//     TestAnalyze_RustTraitImplMethodConfidence, which pin this behavior.
//   - Everything else (including unset/"" language, used by
//     hand-constructed test symbols to mean Go) delegates to
//     langutil.IsExportedForDoc. This matters most for TypeScript,
//     JavaScript, Kotlin, Swift, PHP, C#, and Ruby: their handlers never
//     populate IsPublic, so without this delegation nearly every exported
//     symbol (camelCase or snake_case) fell through to the Go-only
//     uppercase-first convention and was misclassified unexported —
//     high-confidence false-positive dead code project-wide for those
//     languages.
func isSymbolExported(sym *parser.Symbol) bool {
	if sym.IsPublic {
		return true
	}
	if sym.Language == "rust" {
		return false
	}
	language := sym.Language
	if language == "" {
		language = "go"
	}
	return langutil.IsExportedForDoc(sym.Name, language, sym.IsPublic)
}
