package langutil

import (
	"unicode"
	"unicode/utf8"
)

// IsExportedForDoc reports whether a symbol is considered exported for
// doc-coverage purposes, using language-appropriate rules.
//
//   - For JS/TS: any non-underscore name is considered exported (the
//     uppercase-first convention does not apply in those languages).
//   - For Rust/Python/C++ and others: an explicit IsPublic=true flag
//     (e.g. "pub fn", non-underscore Python, "public:" C++ member) is
//     authoritative.
//   - Fallback: the standard Go-style uppercase-first-rune check.
func IsExportedForDoc(name, language string, isPublic bool) bool {
	if isPublic {
		return true
	}
	switch language {
	case "javascript", "typescript":
		if name == "" {
			return false
		}
		// '_' is ASCII 0x5F — single-byte check is safe.
		return name[0] != '_'
	default:
		if name == "" {
			return false
		}
		r, _ := utf8.DecodeRuneInString(name)
		return unicode.IsUpper(r)
	}
}
