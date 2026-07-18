package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// TestConvertStructuralToSymbols_KeepsExpandedSkipsRest asserts the conversion
// keeps matches with Expanded blocks (real symbols) and drops raw matches
// that lack a name/kind — those would render as empty <symbol> entries
// that an agent can't act on.
func TestConvertStructuralToSymbols_KeepsExpandedSkipsRest(t *testing.T) {
	matches := []oxcodes.SearchMatch{
		{
			File: "/repo/internal/auth/login.go", Line: 12,
			Text: "func Login(",
			Expanded: &oxcodes.ExpandedBlock{
				SymbolName: "Login",
				SymbolKind: "function",
				LineStart:  10,
				LineEnd:    25,
				Body:       "func Login(ctx context.Context, name string) error {\n    // ...\n}",
			},
		},
		{
			// No Expanded — must be skipped (no usable name/kind).
			File: "/repo/internal/auth/util.go", Line: 7, Text: "// helper",
		},
	}

	syms := convertStructuralToSymbols(matches, "/repo")
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol (Expanded only), got %d", len(syms))
	}
	got := syms[0]
	if got.Name != "Login" || string(got.Kind) != "function" {
		t.Errorf("name/kind: got %q/%q, want Login/function", got.Name, got.Kind)
	}
	if got.File != "internal/auth/login.go" {
		t.Errorf("file should be relative to root, got %q", got.File)
	}
	if got.StartLine != 10 || got.EndLine != 25 {
		t.Errorf("lines: got %d-%d, want 10-25", got.StartLine, got.EndLine)
	}
	if !strings.Contains(got.Signature, "func Login(ctx context.Context, name string) error") {
		t.Errorf("signature must be first non-empty line of body, got %q", got.Signature)
	}
}

// TestConvertStructuralToSymbols_DedupesByEnclosingSymbol asserts multiple
// pattern hits inside the same function collapse to one symbol entry.
// Without dedup, an agent searching "func $N($$$) error" could see the
// same function listed N times when N internal expressions match.
func TestConvertStructuralToSymbols_DedupesByEnclosingSymbol(t *testing.T) {
	exp := &oxcodes.ExpandedBlock{
		SymbolName: "Process",
		SymbolKind: "function",
		LineStart:  10,
		LineEnd:    50,
		Body:       "func Process() error { ... }",
	}
	matches := []oxcodes.SearchMatch{
		{File: "/repo/x.go", Line: 12, Expanded: exp},
		{File: "/repo/x.go", Line: 25, Expanded: exp},
		{File: "/repo/x.go", Line: 41, Expanded: exp},
	}
	syms := convertStructuralToSymbols(matches, "/repo")
	if len(syms) != 1 {
		t.Fatalf("expected 1 deduped symbol, got %d", len(syms))
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"func F() {\n  body\n}", "func F() {"},
		{"\n\n  type X struct{}\n", "type X struct{}"},
		{"", ""},
		{"\n\n\n", ""},
	}
	for _, c := range cases {
		if got := firstNonEmptyLine(c.in); got != c.want {
			t.Errorf("firstNonEmptyLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
