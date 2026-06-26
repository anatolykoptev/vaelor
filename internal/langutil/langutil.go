package langutil

import (
	"unicode/utf8"
)

// IsExportedForDoc reports whether a symbol is considered exported for
// doc-coverage purposes, using language-appropriate rules. The logic mirrors
// isExportedSymbol in internal/compare/apisurf.go, extended with the IsPublic
// early-return for languages that carry an explicit visibility flag.
//
//   - IsPublic=true always wins — covers explicit "pub fn" (Rust), "def" with
//     __all__ (Python), "public:" (C++), and any other language where the
//     parser sets the flag.
//   - Go, Java, C#: uppercase-first-rune convention.
//   - JavaScript, TypeScript, Rust, Python, and all others: any non-underscore
//     name is exported (matches the non-Go branch in apisurf.isExportedSymbol).
func IsExportedForDoc(name, language string, isPublic bool) bool {
	if isPublic {
		return true
	}
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	switch language {
	case "go", "java", "csharp", "cs":
		return r >= 'A' && r <= 'Z'
	default:
		// JavaScript, TypeScript, Rust, Python, Kotlin, Swift, and others:
		// any non-underscore first rune is considered exported.
		// '_' is ASCII 0x5F — safe to compare as rune.
		return r != '_'
	}
}
