package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRunesBasic verifies that Svelte 5 rune call expressions are classified as
// KindRune with the appropriate RuneKind, using the runes_basic.svelte fixture.
func TestRunesBasic(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "runes_basic.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("runes_basic.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	byName := make(map[string]*parser.Symbol, len(result.Symbols))
	for _, s := range result.Symbols {
		byName[s.Name] = s
	}

	cases := []struct {
		name     string
		runeKind string
	}{
		{"count", "state"},
		{"doubled", "derived"},
		{"raw", "state"},
		{"sum", "derived"},
		{"id", "props"},
		{"val", "bindable"},
	}
	for _, c := range cases {
		sym, ok := byName[c.name]
		if !ok {
			t.Errorf("missing symbol %q; got %v", c.name, runeSymbolNames(result.Symbols))
			continue
		}
		if sym.Kind != parser.KindRune {
			t.Errorf("symbol %q: Kind = %q, want rune", c.name, sym.Kind)
		}
		if sym.RuneKind != c.runeKind {
			t.Errorf("symbol %q: RuneKind = %q, want %q", c.name, sym.RuneKind, c.runeKind)
		}
	}

	// Standalone $effect, $effect.pre, $effect.root should appear as synthetic symbols.
	if len(runeSymbolsWithKind(result.Symbols, "effect")) < 1 {
		t.Errorf("expected at least 1 effect rune symbol, got 0; all: %v", runeSymbolNames(result.Symbols))
	}
	// $inspect and $inspect.with should appear.
	if len(runeSymbolsWithKind(result.Symbols, "inspect")) < 1 {
		t.Errorf("expected at least 1 inspect rune symbol, got 0; all: %v", runeSymbolNames(result.Symbols))
	}
}

// TestRunesNegative verifies that non-rune calls (missing $) are NOT classified.
func TestRunesNegative(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "runes_negative.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("runes_negative.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, s := range result.Symbols {
		if s.Kind == parser.KindRune {
			t.Errorf("unexpected KindRune for symbol %q (no $ prefix case)", s.Name)
		}
	}
}

func runeSymbolNames(syms []*parser.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name+"("+string(s.Kind)+")")
	}
	return names
}

func runeSymbolsWithKind(syms []*parser.Symbol, runeKind string) []*parser.Symbol {
	var out []*parser.Symbol
	for _, s := range syms {
		if s.RuneKind == runeKind {
			out = append(out, s)
		}
	}
	return out
}
