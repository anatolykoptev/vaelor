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

// parseSymbolName extracts the last meaningful name segment from a SCIP symbol string.
// SCIP global symbol format: "<scheme> <manager> <package> <descriptor>..."
// Descriptors are separated by /  #  .  ()  — we want the name part.
// Examples:
//   "scip-typescript npm pkg 1.0.0 `main.ts`/run()." → "run"
//   "scip-typescript npm pkg 1.0.0 `main.ts`/Greeter#greet()." → "greet"
//   "scip-typescript npm pkg 1.0.0 `main.ts`/Greeter#greet().(name)" → "greet"
func parseSymbolName(sym string) string {
	parts := strings.Fields(sym)
	if len(parts) == 0 {
		return sym
	}
	// Join descriptors (everything after scheme+manager+package)
	desc := parts[len(parts)-1]

	// Remove parameter descriptors "(paramName)" at the end
	for strings.HasSuffix(desc, ")") {
		idx := strings.LastIndex(desc, "(")
		if idx <= 0 {
			break
		}
		desc = desc[:idx]
	}

	// Strip trailing punctuation
	desc = strings.TrimRight(desc, ".#/")

	// Take last segment after any separator
	for _, sep := range []string{"/", "#"} {
		if idx := strings.LastIndex(desc, sep); idx >= 0 {
			desc = desc[idx+1:]
		}
	}

	// Strip backticks from file descriptors
	desc = strings.Trim(desc, "`")

	// Final cleanup
	desc = strings.TrimRight(desc, ".#()")
	if desc == "" {
		return parts[len(parts)-1]
	}
	return desc
}

// scipSymbolPkgIndex is the 0-based field index of the package in a SCIP symbol string.
// Format: "<scheme> <manager> <package> <version> <descriptor>..."
const scipSymbolPkgIndex = 2

// pkgFromSymbol extracts the package path from a SCIP symbol string.
func pkgFromSymbol(sym string) string {
	parts := strings.Fields(sym)
	if len(parts) <= scipSymbolPkgIndex {
		return ""
	}
	return parts[scipSymbolPkgIndex]
}

