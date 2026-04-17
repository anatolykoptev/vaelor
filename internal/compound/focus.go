package compound

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// FilterByFocus returns only symbols whose file path matches the focus string.
// Matching strategy: exact → suffix → substring, in that order.
// An empty focus returns all symbols unchanged.
func FilterByFocus(symbols []*parser.Symbol, focus string) []*parser.Symbol {
	if focus == "" {
		return symbols
	}
	// Try exact match first.
	var exact []*parser.Symbol
	for _, sym := range symbols {
		if sym.File == focus {
			exact = append(exact, sym)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	// Try suffix match (e.g. focus="ThemeToggle.svelte").
	var suffix []*parser.Symbol
	for _, sym := range symbols {
		if strings.HasSuffix(sym.File, focus) {
			suffix = append(suffix, sym)
		}
	}
	if len(suffix) > 0 {
		return suffix
	}
	// Fall back to substring match (e.g. focus="components/Filters").
	var sub []*parser.Symbol
	for _, sym := range symbols {
		if strings.Contains(sym.File, focus) {
			sub = append(sub, sym)
		}
	}
	return sub
}
