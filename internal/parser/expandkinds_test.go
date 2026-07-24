package parser_test

import (
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
// definitions as KindMacro when ExpandSymbolKinds is ON. When the flag is OFF,
// the macro symbol is skipped at the parse-time emission chokepoint and does
// NOT appear in pr.Symbols (byte-identical to pre-#664).
//
// Falsification: remove the macro_definition capture from rust.scm → the
// KindMacro assertion goes Red (macro symbol absent from parse result).
// Revert the parse-time gate (emit unconditionally) → the flag-OFF assertion
// goes Red (macro appears when it should not).
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

	// Flag ON — macro extracted.
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
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

	// Flag OFF — macro NOT in pr.Symbols (parse-time gate).
	resultOff, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	if sym := findSymbol(resultOff.Symbols, "vec"); sym != nil {
		t.Errorf("flag OFF: macro vec must NOT be in pr.Symbols, got kind %q", sym.Kind)
	}
}

// TestParseRustModuleExtraction verifies the .scm query extracts mod
// declarations as KindModule when ExpandSymbolKinds is ON. When the flag is
// OFF, the module symbol is skipped at the parse-time emission chokepoint and
// does NOT appear in pr.Symbols (byte-identical to pre-#664). This is a NEW
// capture — mod_item was not previously extracted as a standalone symbol (only
// functions inside mod blocks were captured).
//
// Falsification: remove the mod_item capture from rust.scm → the KindModule
// assertion goes Red. Revert the parse-time gate → the flag-OFF assertion
// goes Red (module appears when it should not).
func TestParseRustModuleExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`pub mod network {
    pub fn connect() {}
}

mod util;
`)

	// Flag ON — module extracted.
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
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

	// Flag OFF — module NOT in pr.Symbols (parse-time gate).
	resultOff, err := parser.ParseFile("test.rs", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	if sym := findSymbol(resultOff.Symbols, "network"); sym != nil {
		t.Errorf("flag OFF: module network must NOT be in pr.Symbols, got kind %q", sym.Kind)
	}
}

// TestParseCMacroExtraction verifies the .scm query extracts both object-like
// (#define MAX_BUFFER) and function-like (#define SQUARE(x)) macros as
// KindMacro in C source when ExpandSymbolKinds is ON. When the flag is OFF,
// neither macro appears in pr.Symbols (parse-time gate).
//
// Falsification: remove the preproc_def capture from c.scm → the MAX_BUFFER
// assertion goes Red. Remove the preproc_function_def capture → the SQUARE
// assertion goes Red. Revert the parse-time gate → the flag-OFF assertions
// go Red (macros appear when they should not).
func TestParseCMacroExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`#define MAX_BUFFER 4096
#define SQUARE(x) ((x) * (x))

int main(void) { return 0; }
`)

	// Flag ON — both macros extracted.
	result, err := parser.ParseFile("test.c", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
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
	// Function-like macro — preproc_function_def capture (#664).
	symSquare := findSymbol(result.Symbols, "SQUARE")
	if symSquare == nil {
		t.Fatalf("#define SQUARE(x) not found; got %d symbols", len(result.Symbols))
	}
	if symSquare.Kind != parser.KindMacro {
		t.Errorf("SQUARE kind = %q, want %q", symSquare.Kind, parser.KindMacro)
	}

	// Flag OFF — neither macro in pr.Symbols (parse-time gate).
	resultOff, err := parser.ParseFile("test.c", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	if sym := findSymbol(resultOff.Symbols, "MAX_BUFFER"); sym != nil {
		t.Errorf("flag OFF: MAX_BUFFER must NOT be in pr.Symbols, got kind %q", sym.Kind)
	}
	if sym := findSymbol(resultOff.Symbols, "SQUARE"); sym != nil {
		t.Errorf("flag OFF: SQUARE must NOT be in pr.Symbols, got kind %q", sym.Kind)
	}
}

// TestParseCppMacroExtraction verifies the .scm query extracts #define as
// KindMacro in C++ source when ExpandSymbolKinds is ON. When the flag is OFF,
// the macro does NOT appear in pr.Symbols (parse-time gate).
//
// Falsification: remove the preproc_def capture from cpp.scm → the KindMacro
// assertion goes Red. Revert the parse-time gate → the flag-OFF assertion
// goes Red (macro appears when it should not).
func TestParseCppMacroExtraction(t *testing.T) {
	t.Parallel()
	source := []byte(`#define PI 3.14159

int main() { return 0; }
`)

	// Flag ON — macro extracted.
	result, err := parser.ParseFile("test.cpp", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
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

	// Flag OFF — macro NOT in pr.Symbols (parse-time gate).
	resultOff, err := parser.ParseFile("test.cpp", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	if sym := findSymbol(resultOff.Symbols, "PI"); sym != nil {
		t.Errorf("flag OFF: PI must NOT be in pr.Symbols, got kind %q", sym.Kind)
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

// TestParseCTypeAliasRefinement verifies that C typedefs (type_definition) are
// refined from KindType to KindTypeAlias when ExpandSymbolKinds is set. When
// the flag is OFF, they remain KindType (byte-identical to pre-#664).
//
// Falsification: remove the type_definition case from typeAliasNodeTypes →
// the expanded=true assertion goes Red (stays KindType).
func TestParseCTypeAliasRefinement(t *testing.T) {
	t.Parallel()
	source := []byte(`typedef int Handle;

struct Server { int port; };
`)

	// Flag OFF — typedef stays KindType.
	resultOff, err := parser.ParseFile("test.c", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	symOff := findSymbol(resultOff.Symbols, "Handle")
	if symOff == nil {
		t.Fatalf("typedef Handle not found (flag off); got %d symbols", len(resultOff.Symbols))
	}
	if symOff.Kind != parser.KindType {
		t.Errorf("flag OFF: Handle kind = %q, want %q", symOff.Kind, parser.KindType)
	}

	// Flag ON — refined to KindTypeAlias.
	resultOn, err := parser.ParseFile("test.c", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
	if err != nil {
		t.Fatalf("ParseFile (flag on): %v", err)
	}
	symOn := findSymbol(resultOn.Symbols, "Handle")
	if symOn == nil {
		t.Fatalf("typedef Handle not found (flag on); got %d symbols", len(resultOn.Symbols))
	}
	if symOn.Kind != parser.KindTypeAlias {
		t.Errorf("flag ON: Handle kind = %q, want %q", symOn.Kind, parser.KindTypeAlias)
	}
}

// TestParseCppTypeAliasRefinement verifies that C++ using-aliases
// (alias_declaration) are refined from KindType to KindTypeAlias when
// ExpandSymbolKinds is set. When the flag is OFF, they remain KindType
// (byte-identical to pre-#664).
//
// Falsification: remove the alias_declaration case from typeAliasNodeTypes →
// the expanded=true assertion goes Red (stays KindType).
func TestParseCppTypeAliasRefinement(t *testing.T) {
	t.Parallel()
	source := []byte(`using Vector = std::vector<int>;

class Config {};
`)

	// Flag OFF — using-alias stays KindType.
	resultOff, err := parser.ParseFile("test.cpp", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile (flag off): %v", err)
	}
	symOff := findSymbol(resultOff.Symbols, "Vector")
	if symOff == nil {
		t.Fatalf("using Vector not found (flag off); got %d symbols", len(resultOff.Symbols))
	}
	if symOff.Kind != parser.KindType {
		t.Errorf("flag OFF: Vector kind = %q, want %q", symOff.Kind, parser.KindType)
	}

	// Flag ON — refined to KindTypeAlias.
	resultOn, err := parser.ParseFile("test.cpp", source, parser.ParseOpts{
		ExpandSymbolKinds: true,
	})
	if err != nil {
		t.Fatalf("ParseFile (flag on): %v", err)
	}
	symOn := findSymbol(resultOn.Symbols, "Vector")
	if symOn == nil {
		t.Fatalf("using Vector not found (flag on); got %d symbols", len(resultOn.Symbols))
	}
	if symOn.Kind != parser.KindTypeAlias {
		t.Errorf("flag ON: Vector kind = %q, want %q", symOn.Kind, parser.KindTypeAlias)
	}
}

// TestParseNonRegression_FlagOff verifies that with ExpandSymbolKinds=false
// (the default), the parsed symbol set for a Rust file with macros/modules is
// byte-identical to the pre-#664 parse result: the new symbols (macro, module)
// are NOT in pr.Symbols at all (skipped at the parse-time emission chokepoint),
// and existing symbols' kinds are unchanged. This is the parser-level
// non-regression guard that covers ALL consumers (codegraph, explore, ingest,
// embeddings) — not just the embeddings path.
//
// Falsification: revert the parse-time gate in processCaptureWithCaps (emit
// macro/module unconditionally) → the "helper"/"inner" absent assertions go
// Red (the new symbols appear in pr.Symbols when they should not).
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

	// New symbols (macro, module) are NOT in pr.Symbols when flag OFF — the
	// parse-time gate in processCaptureWithCaps skips them entirely. This is
	// the load-bearing guard: ALL consumers read pr.Symbols, so absence here
	// means codegraph/explore/ingest/embeddings are all byte-identical.
	if k, ok := byName["helper"]; ok {
		t.Errorf("flag OFF: macro helper must NOT be in pr.Symbols (parse-time gate), got kind %q", k)
	}
	if k, ok := byName["inner"]; ok {
		t.Errorf("flag OFF: module inner must NOT be in pr.Symbols (parse-time gate), got kind %q", k)
	}
}

// TestParseTimeGate_AllConsumers is the load-bearing guard for the parse-time
// extraction gate (#664). It asserts directly on pr.Symbols — the shared struct
// ALL consumers read (codegraph/graph_build.go buildSymbolGraph adds a vertex
// per symbol with NO kind filter; explore/parse.go and ingest/focus.go append
// pr.Symbols verbatim; embeddings filters via IsEmbeddableKindExpanded). When
// ExpandSymbolKinds is OFF, pr.Symbols must contain ZERO KindMacro/KindModule
// symbols for C, C++, and Rust — byte-identical to the pre-#664 parse result.
// When ON, the new kinds are present.
//
// Falsification: revert the parse-time gate in processCaptureWithCaps (emit
// macro/module unconditionally) → every flag-OFF assertion goes Red (the new
// symbols appear in pr.Symbols when they should not).
func TestParseTimeGate_AllConsumers(t *testing.T) {
	t.Parallel()

	cSource := []byte(`#define MAX_BUFFER 4096
#define SQUARE(x) ((x) * (x))

int main(void) { return 0; }
`)
	cppSource := []byte(`#define PI 3.14159
#define ABS(x) ((x) < 0 ? -(x) : (x))

int main() { return 0; }
`)
	rustSource := []byte(`pub macro_rules! helper { () => {} }

pub mod network {
    pub fn connect() {}
}

pub fn init() {}
`)

	assertNoNewKinds := func(t *testing.T, syms []*parser.Symbol) {
		t.Helper()
		for _, s := range syms {
			if s.Kind == parser.KindMacro {
				t.Errorf("flag OFF: KindMacro %q must NOT be in pr.Symbols (parse-time gate)", s.Name)
			}
			if s.Kind == parser.KindModule {
				t.Errorf("flag OFF: KindModule %q must NOT be in pr.Symbols (parse-time gate)", s.Name)
			}
		}
	}

	// Flag OFF — no macro/module symbols in pr.Symbols for any language.
	t.Run("C_flagOff", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.c", cSource, parser.ParseOpts{})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		assertNoNewKinds(t, pr.Symbols)
	})
	t.Run("Cpp_flagOff", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.cpp", cppSource, parser.ParseOpts{})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		assertNoNewKinds(t, pr.Symbols)
	})
	t.Run("Rust_flagOff", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.rs", rustSource, parser.ParseOpts{})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		assertNoNewKinds(t, pr.Symbols)
	})

	// Flag ON — macro/module symbols present in pr.Symbols.
	t.Run("C_flagOn", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.c", cSource, parser.ParseOpts{ExpandSymbolKinds: true})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if findSymbol(pr.Symbols, "MAX_BUFFER") == nil {
			t.Errorf("flag ON: MAX_BUFFER macro must be in pr.Symbols")
		}
		if findSymbol(pr.Symbols, "SQUARE") == nil {
			t.Errorf("flag ON: SQUARE macro must be in pr.Symbols")
		}
	})
	t.Run("Cpp_flagOn", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.cpp", cppSource, parser.ParseOpts{ExpandSymbolKinds: true})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if findSymbol(pr.Symbols, "PI") == nil {
			t.Errorf("flag ON: PI macro must be in pr.Symbols")
		}
		if findSymbol(pr.Symbols, "ABS") == nil {
			t.Errorf("flag ON: ABS macro must be in pr.Symbols")
		}
	})
	t.Run("Rust_flagOn", func(t *testing.T) {
		t.Parallel()
		pr, err := parser.ParseFile("test.rs", rustSource, parser.ParseOpts{ExpandSymbolKinds: true})
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if findSymbol(pr.Symbols, "helper") == nil {
			t.Errorf("flag ON: helper macro must be in pr.Symbols")
		}
		if findSymbol(pr.Symbols, "network") == nil {
			t.Errorf("flag ON: network module must be in pr.Symbols")
		}
	})
}

func findSymbol(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, s := range symbols {
		if s.Name == name {
			return s
		}
	}
	return nil
}
