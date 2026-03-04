package deadcode

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
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

// isSymbolExported returns true if the symbol is public/exported.
// Uses IsPublic field for languages that set it (Rust), falls back to Go uppercase convention.
func isSymbolExported(sym *parser.Symbol) bool {
	if sym.IsPublic {
		return true
	}
	if sym.Language == "rust" {
		return false
	}
	return isExported(sym.Name)
}
