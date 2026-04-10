package compare

import (
	"path/filepath"
	"unicode/utf8"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// APISymbol represents a single exported symbol in a repository's public API surface.
type APISymbol struct {
	Name      string
	Kind      string
	Signature string
	Package   string
	File      string
}

// APIDelta captures a signature change for a symbol present in both repos.
type APIDelta struct {
	Name string
	Kind string
	SigA string
	SigB string
}

// APIDiff is the result of comparing two API surfaces.
type APIDiff struct {
	Common     int
	OnlyACount int
	OnlyBCount int
	ChangedSig int
	OnlyA      []APISymbol
	OnlyB      []APISymbol
	Changed    []APIDelta
}

// isExportedSymbol reports whether a symbol name is considered exported in the given language.
//
// For Go, Java, and C#, a symbol is exported when its first rune is uppercase.
// For Python, JS/TS, and Rust, a symbol is exported when its first rune is not an underscore.
func isExportedSymbol(name, language string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	switch language {
	case "go", "java", "csharp", "cs":
		return r >= 'A' && r <= 'Z'
	default:
		// Python, JavaScript, TypeScript, Rust and others.
		return r != '_'
	}
}

// apiKinds is the set of symbol kinds that form part of the public API surface.
var apiKinds = map[string]bool{
	"function":  true,
	"method":    true,
	"type":      true,
	"interface": true,
}

// ExtractAPISurface filters a symbol list to exported symbols of relevant kinds.
// Package is set to the directory of the symbol's file.
func ExtractAPISurface(symbols []*parser.Symbol, language string) []APISymbol {
	result := make([]APISymbol, 0, len(symbols))
	for _, sym := range symbols {
		if sym == nil {
			continue
		}
		kind := string(sym.Kind)
		if !apiKinds[kind] {
			continue
		}
		if !isExportedSymbol(sym.Name, language) {
			continue
		}
		result = append(result, APISymbol{
			Name:      sym.Name,
			Kind:      kind,
			Signature: sym.Signature,
			Package:   filepath.Dir(sym.File),
			File:      sym.File,
		})
	}
	return result
}

// apiKey builds a lookup key for an APISymbol from its name and kind.
func apiKey(s APISymbol) string {
	return s.Name + "\x00" + s.Kind
}

// ComputeAPIDiff compares two API surfaces and returns a structured diff.
// Symbols are matched by (name, kind) key. Matched symbols with different
// signatures are counted as changed.
func ComputeAPIDiff(a, b []APISymbol) APIDiff {
	indexA := make(map[string]APISymbol, len(a))
	for _, sym := range a {
		indexA[apiKey(sym)] = sym
	}

	indexB := make(map[string]APISymbol, len(b))
	for _, sym := range b {
		indexB[apiKey(sym)] = sym
	}

	var diff APIDiff

	for key, symA := range indexA {
		symB, exists := indexB[key]
		if !exists {
			diff.OnlyA = append(diff.OnlyA, symA)
			diff.OnlyACount++
			continue
		}
		diff.Common++
		if symA.Signature != symB.Signature {
			diff.ChangedSig++
			diff.Changed = append(diff.Changed, APIDelta{
				Name: symA.Name,
				Kind: symA.Kind,
				SigA: symA.Signature,
				SigB: symB.Signature,
			})
		}
	}

	for key, symB := range indexB {
		if _, exists := indexA[key]; !exists {
			diff.OnlyB = append(diff.OnlyB, symB)
			diff.OnlyBCount++
		}
	}

	return diff
}
