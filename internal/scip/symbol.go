package scip

import (
	"strings"

	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

// defInfo holds resolved definition metadata for a SCIP symbol.
type defInfo struct {
	Name string
	File string
	Line uint32
	Pkg  string
}

// funcRange describes the line span of a function definition.
type funcRange struct {
	Name      string
	StartLine uint32
	EndLine   uint32 // inclusive; 0 = unknown (open-ended)
}

// isFuncKind reports whether k is a callable symbol kind.
func isFuncKind(k sciplib.SymbolInformation_Kind) bool {
	switch k {
	case sciplib.SymbolInformation_Function,
		sciplib.SymbolInformation_Method,
		sciplib.SymbolInformation_Constructor,
		sciplib.SymbolInformation_MethodAlias,
		sciplib.SymbolInformation_MethodReceiver,
		sciplib.SymbolInformation_MethodSpecification:
		return true
	}
	return false
}

// isFuncSymbol uses a heuristic on the SCIP symbol string when Kind is unavailable.
// Go SCIP symbols for functions end with "()" or "()." in the descriptor segment.
func isFuncSymbol(sym string) bool {
	return strings.HasSuffix(sym, "().") || strings.HasSuffix(sym, "()")
}

// nameFromDisplayOrSymbol extracts a human-readable name.
// It prefers displayName; if empty it parses the last segment of the SCIP symbol string.
func nameFromDisplayOrSymbol(displayName, sym string) string {
	if displayName != "" {
		return displayName
	}
	return parseSymbolName(sym)
}

// parseSymbolName extracts the last meaningful name segment from a SCIP symbol string.
// SCIP global symbol format: "<scheme> <manager> <package> <descriptor>..."
// We take the last whitespace-delimited token and strip descriptor suffixes.
func parseSymbolName(sym string) string {
	parts := strings.Fields(sym)
	if len(parts) == 0 {
		return sym
	}
	last := parts[len(parts)-1]
	// Strip trailing descriptor punctuation: "().", "()", "#", "."
	last = strings.TrimRight(last, ".#()")
	return last
}

// pkgFromSymbol extracts the package path from a SCIP symbol string.
// Format: "<scheme> <manager> <package> ..."  — 3rd field (index 2).
func pkgFromSymbol(sym string) string {
	parts := strings.Fields(sym)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// buildSymbolLookup builds a map from symbol string to SymbolInformation
// for the symbols declared in a document.
func buildSymbolLookup(doc interface{ GetSymbols() []*sciplib.SymbolInformation }) map[string]*sciplib.SymbolInformation {
	syms := doc.GetSymbols()
	m := make(map[string]*sciplib.SymbolInformation, len(syms))
	for _, si := range syms {
		if si != nil {
			m[si.Symbol] = si
		}
	}
	return m
}
