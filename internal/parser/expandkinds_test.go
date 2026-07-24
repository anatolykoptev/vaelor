package parser_test

import (
	"sort"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestIsEmbeddableKindExpanded verifies the flag-gated predicate that controls
// whether the new low-volume symbol kinds (macro, module, type-alias) enter the
// embedding index. When expanded=false the predicate is byte-identical to
// IsEmbeddableKind (prod unchanged). When expanded=true the new kinds are
// admitted alongside the historical set.
//
// Falsification: revert IsEmbeddableKindExpanded to always delegate to
// IsEmbeddableKind (drop the expanded switch) → the expanded=true assertions
// for KindMacro/KindModule/KindTypeAlias go RED.
func TestIsEmbeddableKindExpanded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind     parser.NodeKind
		expanded bool
		want     bool
	}{
		// Historical kinds — always embeddable, regardless of flag.
		{parser.KindFunction, false, true},
		{parser.KindMethod, false, true},
		{parser.KindType, false, true},
		{parser.KindStruct, false, true},
		{parser.KindInterface, false, true},
		{parser.KindClass, false, true},
		{parser.KindFunction, true, true},
		{parser.KindType, true, true},

		// New kinds — excluded when flag OFF, included when flag ON.
		{parser.KindMacro, false, false},
		{parser.KindMacro, true, true},
		{parser.KindModule, false, false},
		{parser.KindModule, true, true},
		{parser.KindTypeAlias, false, false},
		{parser.KindTypeAlias, true, true},

		// Always-excluded kinds — flag has no effect.
		{parser.KindConst, false, false},
		{parser.KindConst, true, false},
		{parser.KindVar, false, false},
		{parser.KindVar, true, false},
		{parser.KindImport, false, false},
		{parser.KindImport, true, false},
		{parser.KindRune, false, false},
		{parser.KindRune, true, false},

		// Unknown kind — never embeddable.
		{"", false, false},
		{"", true, false},
		{"unknown", false, false},
		{"unknown", true, false},
	}
	for _, tt := range tests {
		label := string(tt.kind) + "/expanded=" + boolStr(tt.expanded)
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := parser.IsEmbeddableKindExpanded(tt.kind, tt.expanded)
			if got != tt.want {
				t.Errorf("IsEmbeddableKindExpanded(%q, %v) = %v, want %v",
					tt.kind, tt.expanded, got, tt.want)
			}
		})
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestParseRustMacroExtraction verifies the .scm query extracts macro_rules!
// definitions as KindMacro. This is a NEW capture — macro_rules! was not
// previously extracted at all, so flag OFF has no regression (the symbol simply
// does not appear in the embed set; it IS in the parse result).
//
// Falsification: remove the macro_definition capture from rust.scm → the
// KindMacro assertion goes Red (macro symbol absent from parse result).
func TestParseRustMacroExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`pub macro_rules! vec {
    ($($x:expr),*) => {{
        let mut v = Vec::new();
        $(v.push($x);)*
        v
    }};
}
`)
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	sym := findSymbol(result.Symbols, "vec")
	if sym == nil {
		t.Fatalf("macro_rules! vec not found in symbols; got %d symbols", len(result.Symbols))
	}
	if sym.Kind != parser.KindMacro {
		t.Errorf("macro vec kind = %q, want %q", sym.Kind, parser.KindMacro)
	}
	if sym.Language != "rust" {
		t.Errorf("macro vec language = %q, want rust", sym.Language)
	}
}

// TestParseRustModuleExtraction verifies the .scm query extracts mod
// declarations as KindModule. This is a NEW capture — mod_item was not
// previously extracted as a standalone symbol (only functions inside mod blocks
// were captured).
//
// Falsification: remove the mod_item capture from rust.scm → the KindModule
// assertion goes Red.
func TestParseRustModuleExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`pub mod network {
    pub fn connect() {}
}

mod util;
`)
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	sym := findSymbol(result.Symbols, "network")
	if sym == nil {
		t.Fatalf("mod network not found in symbols; got %d symbols", len(result.Symbols))
	}
	if sym.Kind != parser.KindModule {
		t.Errorf("mod network kind = %q, want %q", sym.Kind, parser.KindModule)
	}
}

// TestParseCMacroExtraction verifies the .scm query extracts #define as
// KindMacro in C source.
//
// Falsification: remove the preproc_def capture from c.scm → the KindMacro
// assertion goes Red.
func TestParseCMacroExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`#define MAX_BUFFER 4096
#define SQUARE(x) ((x) * (x))

int main(void) { return 0; }
`)
	result, err := parser.ParseFile("test.c", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	sym := findSymbol(result.Symbols, "MAX_BUFFER")
	if sym == nil {
		t.Fatalf("#define MAX_BUFFER not found; got %d symbols", len(result.Symbols))
	}
	if sym.Kind != parser.KindMacro {
		t.Errorf("MAX_BUFFER kind = %q, want %q", sym.Kind, parser.KindMacro)
	}
}

// TestParseCppMacroExtraction verifies the .scm query extracts #define as
// KindMacro in C++ source.
//
// Falsification: remove the preproc_def capture from cpp.scm → the KindMacro
// assertion goes Red.
func TestParseCppMacroExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`#define PI 3.14159

int main() { return 0; }
`)
	result, err := parser.ParseFile("test.cpp", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	sym := findSymbol(result.Symbols, "PI")
	if sym == nil {
		t.Fatalf("#define PI not found; got %d symbols", len(result.Symbols))
	}
	if sym.Kind != parser.KindMacro {
		t.Errorf("PI kind = %q, want %q", sym.Kind, parser.KindMacro)
	}
}

// TestParseRustTypeAliasRefinement verifies that when ExpandSymbolKinds is set
// in ParseOpts, Rust type aliases (type_item) are refined from KindType to
// KindTypeAlias. When the flag is OFF, they remain KindType (byte-identical to
// pre-change behavior).
//
// Falsification: remove the refineExpandedKind call from processCaptureWithCaps
// → the expanded=true assertion goes Red (stays KindType). Revert the flag
// check → the expanded=false assertion goes Red (becomes KindTypeAlias
// prematurely).
func TestParseRustTypeAliasRefinement(t *testing.T) {
	t.Parallel()
	source := []byte(`pub type Result<T> = std::result::Result<T, MyError>;

pub struct MyError { msg: String }
`)

	// Flag OFF — type alias stays KindType (byte-identical to today).
	resultOff, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	symOff := findSymbol(resultOff.Symbols, "Result")
	if symOff == nil {
		t.Fatalf("type Result not found (flag off); got %d symbols", len(resultOff.Symbols))
	}
	if symOff.Kind != parser.KindType {
		t.Errorf("flag OFF: type Result kind = %q, want %q (byte-identical to pre-change)",
			symOff.Kind, parser.KindType)
	}

	// Flag ON — type alias refined to KindTypeAlias.
	resultOn, err := parser.ParseFile("test.rs", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
	if err != nil {
		t.Fatalf("ParseFile (flag on): %v", err)
	}
	symOn := findSymbol(resultOn.Symbols, "Result")
	if symOn == nil {
		t.Fatalf("type Result not found (flag on); got %d symbols", len(resultOn.Symbols))
	}
	if symOn.Kind != parser.KindTypeAlias {
		t.Errorf("flag ON: type Result kind = %q, want %q", symOn.Kind, parser.KindTypeAlias)
	}
}

// TestParseTSTypeAliasRefinement verifies TypeScript type aliases are refined
// to KindTypeAlias when ExpandSymbolKinds is set.
//
// Falsification: remove the type_alias_declaration case from refineExpandedKind
// → the expanded=true assertion goes Red.
func TestParseTSTypeAliasRefinement(t *testing.T) {
	t.Parallel()
	source := []byte(`export type UserID = number;
`)

	// Flag OFF — stays KindType.
	resultOff, err := parser.ParseFile("test.ts", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	symOff := findSymbol(resultOff.Symbols, "UserID")
	if symOff == nil {
		t.Fatalf("type UserID not found (flag off); got %d symbols", len(resultOff.Symbols))
	}
	if symOff.Kind != parser.KindType {
		t.Errorf("flag OFF: type UserID kind = %q, want %q", symOff.Kind, parser.KindType)
	}

	// Flag ON — refined to KindTypeAlias.
	resultOn, err := parser.ParseFile("test.ts", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
	if err != nil {
		t.Fatalf("ParseFile (flag on): %v", err)
	}
	symOn := findSymbol(resultOn.Symbols, "UserID")
	if symOn == nil {
		t.Fatalf("type UserID not found (flag on); got %d symbols", len(resultOn.Symbols))
	}
	if symOn.Kind != parser.KindTypeAlias {
		t.Errorf("flag ON: type UserID kind = %q, want %q", symOn.Kind, parser.KindTypeAlias)
	}
}

// TestParseNonRegression_FlagOff verifies that with ExpandSymbolKinds=false
// (the default), the parsed symbol set for a Rust file with macros/modules is
// a superset that includes the new symbols but the EXISTING symbols are
// byte-identical in kind. The new symbols (macro/module) ARE in the parse
// result (the .scm always captures them) but are excluded from the embed set
// by IsEmbeddableKindExpanded(kind, false).
//
// This is the parser-level non-regression guard: existing symbols' kinds are
// unchanged when the flag is OFF.
func TestParseNonRegression_FlagOff(t *testing.T) {
	t.Parallel()
	source := []byte(`pub macro_rules! helper { () => {} }

pub mod inner {
    pub fn work() {}
}

pub type Alias = u32;

pub fn main_fn() {}
`)
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]parser.NodeKind, len(result.Symbols))
	for _, s := range result.Symbols {
		byName[s.Name] = s.Kind
	}

	// Existing symbols — kinds unchanged.
	if k, ok := byName["main_fn"]; !ok || k != parser.KindFunction {
		t.Errorf("main_fn kind = %q (ok=%v), want function", k, ok)
	}
	if k, ok := byName["work"]; !ok || k != parser.KindFunction {
		t.Errorf("work kind = %q (ok=%v), want function", k, ok)
	}
	// Type alias stays KindType when flag OFF (byte-identical).
	if k, ok := byName["Alias"]; !ok || k != parser.KindType {
		t.Errorf("Alias kind = %q (ok=%v), want type (byte-identical when flag off)", k, ok)
	}

	// New symbols ARE parsed (the .scm captures them) but are NOT embeddable
	// when flag OFF — the embeddings pipeline filters them out.
	if k, ok := byName["helper"]; !ok {
		t.Errorf("macro helper should be parsed (in .scm), but not found")
	} else if k != parser.KindMacro {
		t.Errorf("macro helper kind = %q, want macro", k)
	}
	if !parser.IsEmbeddableKindExpanded(parser.KindMacro, false) {
		// Confirm the predicate excludes it — this is the non-regression guard.
	} else {
		t.Errorf("KindMacro must NOT be embeddable when flag OFF")
	}

	// Sorted symbol names for a stable snapshot.
	names := make([]string, 0, len(result.Symbols))
	for _, s := range result.Symbols {
		names = append(names, s.Name)
	}
	sort.Strings(names)
}

func findSymbol(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, s := range symbols {
		if s.Name == name {
			return s
		}
	}
	return nil
}
