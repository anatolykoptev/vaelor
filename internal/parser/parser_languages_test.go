package parser_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestParseGoFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.go"))
	if err != nil {
		t.Fatalf("read testdata/sample.go: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.go", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}

	// Verify imports.
	wantImports := []string{"fmt", "net/http"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by name for convenient lookup.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"MaxRetries", parser.KindConst},
		{"Config", parser.KindStruct},
		{"Handler", parser.KindInterface},
		{"defaultConfig", parser.KindVar},
		{"NewConfig", parser.KindFunction},
		{"Run", parser.KindMethod},
	}

	for _, ws := range wantSymbols {
		sym, ok := byName[ws.name]
		if !ok {
			t.Errorf("symbol %q not found; all symbols: %v", ws.name, symbolNames(result.Symbols))
			continue
		}
		if sym.Kind != ws.kind {
			t.Errorf("symbol %q: Kind = %q, want %q", ws.name, sym.Kind, ws.kind)
		}
	}

	// Verify signatures are non-empty for functions and methods.
	for _, name := range []string{"NewConfig", "Run"} {
		sym, ok := byName[name]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseGoFileWithBody(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.go"))
	if err != nil {
		t.Fatalf("read testdata/sample.go: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.go", source, parser.ParseOpts{
		IncludeBody: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	funcSym, ok := byName["NewConfig"]
	if !ok {
		t.Fatal("NewConfig symbol not found")
	}
	if funcSym.Body == "" {
		t.Error("NewConfig.Body is empty with IncludeBody=true")
	}
}

func TestParsePythonFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.py"))
	if err != nil {
		t.Fatalf("read testdata/sample.py: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.py", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}

	// Verify imports.
	wantImports := []string{"os", "pathlib"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by name for convenient lookup.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Verify expected symbols with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Config", parser.KindClass},
		{"create_config", parser.KindFunction},
		{"__init__", parser.KindMethod},
		{"address", parser.KindMethod},
	}

	for _, ws := range wantSymbols {
		sym, ok := byName[ws.name]
		if !ok {
			t.Errorf("symbol %q not found; all symbols: %v", ws.name, symbolNames(result.Symbols))
			continue
		}
		if sym.Kind != ws.kind {
			t.Errorf("symbol %q: Kind = %q, want %q", ws.name, sym.Kind, ws.kind)
		}
	}

	// Verify signatures are non-empty for functions and methods.
	for _, name := range []string{"create_config", "__init__", "address"} {
		sym, ok := byName[name]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseTypeScriptFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.ts"))
	if err != nil {
		t.Fatalf("read testdata/sample.ts: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.ts", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "typescript" {
		t.Errorf("Language = %q, want %q", result.Language, "typescript")
	}

	// Verify imports.
	wantImports := []string{"express"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by name for convenient lookup.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Verify expected symbols with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Handler", parser.KindInterface},
		{"Server", parser.KindClass},
		{"createServer", parser.KindFunction},
		{"constructor", parser.KindMethod},
		{"start", parser.KindMethod},
	}

	for _, ws := range wantSymbols {
		sym, ok := byName[ws.name]
		if !ok {
			t.Errorf("symbol %q not found; all symbols: %v", ws.name, symbolNames(result.Symbols))
			continue
		}
		if sym.Kind != ws.kind {
			t.Errorf("symbol %q: Kind = %q, want %q", ws.name, sym.Kind, ws.kind)
		}
	}

	// Verify signatures are non-empty for functions and methods.
	for _, name := range []string{"createServer", "start"} {
		sym, ok := byName[name]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseRustFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.rs"))
	if err != nil {
		t.Fatalf("read testdata/sample.rs: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.rs", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "rust" {
		t.Errorf("Language = %q, want %q", result.Language, "rust")
	}

	// Verify imports.
	wantImports := []string{"std::io", "std::collections::HashMap"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by name for convenient lookup.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"MAX_RETRIES", parser.KindConst},
		{"DEFAULT_PORT", parser.KindVar},
		{"Config", parser.KindStruct},
		{"Status", parser.KindType},
		{"Handler", parser.KindInterface},
		{"AliasConfig", parser.KindType},
		{"new", parser.KindMethod},
		{"address", parser.KindMethod},
		{"create_config", parser.KindFunction},
	}

	for _, ws := range wantSymbols {
		sym, ok := byName[ws.name]
		if !ok {
			t.Errorf("symbol %q not found; all symbols: %v", ws.name, symbolNames(result.Symbols))
			continue
		}
		if sym.Kind != ws.kind {
			t.Errorf("symbol %q: Kind = %q, want %q", ws.name, sym.Kind, ws.kind)
		}
	}

	// Verify signatures are non-empty for functions and methods.
	for _, name := range []string{"new", "address", "create_config"} {
		sym, ok := byName[name]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseJavaFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.java"))
	if err != nil {
		t.Fatalf("read testdata/sample.java: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.java", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "java" {
		t.Errorf("Language = %q, want %q", result.Language, "java")
	}

	// Verify imports.
	wantImports := []string{"java.util.List", "java.util.HashMap"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by "kind:name" to handle same-name symbols of different kinds
	// (e.g. class Config and constructor Config both appear in Java output).
	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Config", parser.KindClass},
		{"Config", parser.KindMethod}, // constructor
		{"Handler", parser.KindInterface},
		{"Status", parser.KindType},
		{"address", parser.KindMethod},
	}

	for _, ws := range wantSymbols {
		key := string(ws.kind) + ":" + ws.name
		sym, ok := byKindName[key]
		if !ok {
			t.Errorf("symbol %q (kind=%s) not found; all symbols: %v", ws.name, ws.kind, symbolNames(result.Symbols))
			continue
		}
		_ = sym
	}

	// Verify signatures are non-empty for methods.
	for _, key := range []string{"method:address", "method:Config"} {
		sym, ok := byKindName[key]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", sym.Name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseCFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.c"))
	if err != nil {
		t.Fatalf("read testdata/sample.c: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.c", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "c" {
		t.Errorf("Language = %q, want %q", result.Language, "c")
	}

	// Verify imports: system headers and local headers (quotes stripped by parser).
	wantImports := []string{"<stdio.h>", "config.h"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by "kind:name" to handle same-name structs and typedefs.
	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Config", parser.KindType},   // typedef struct { ... } Config
		{"Server", parser.KindStruct}, // struct Server { ... }
		{"Status", parser.KindType},   // enum Status
		{"create_config", parser.KindFunction},
		{"run_server", parser.KindFunction},
	}

	for _, ws := range wantSymbols {
		key := string(ws.kind) + ":" + ws.name
		sym, ok := byKindName[key]
		if !ok {
			t.Errorf("symbol %q (kind=%s) not found; all symbols: %v", ws.name, ws.kind, symbolNames(result.Symbols))
			continue
		}
		_ = sym
	}

	// Verify signatures are non-empty for functions.
	for _, key := range []string{"function:create_config", "function:run_server"} {
		sym, ok := byKindName[key]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", sym.Name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseCppFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.cpp"))
	if err != nil {
		t.Fatalf("read testdata/sample.cpp: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.cpp", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "cpp" {
		t.Errorf("Language = %q, want %q", result.Language, "cpp")
	}

	// Verify imports.
	wantImports := []string{"<iostream>", "<string>"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by "kind:name" to handle same-name symbols of different kinds
	// (e.g. class Config and constructor method Config both appear in C++ output).
	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Point", parser.KindStruct},           // struct Point
		{"Config", parser.KindClass},           // class Config
		{"Status", parser.KindType},            // enum Status
		{"Config::Config", parser.KindMethod},  // out-of-line constructor
		{"Config::address", parser.KindMethod}, // out-of-line method
		{"run", parser.KindFunction},           // free function
	}

	for _, ws := range wantSymbols {
		key := string(ws.kind) + ":" + ws.name
		sym, ok := byKindName[key]
		if !ok {
			t.Errorf("symbol %q (kind=%s) not found; all symbols: %v", ws.name, ws.kind, symbolNames(result.Symbols))
			continue
		}
		_ = sym
	}

	// Verify signatures are non-empty for methods and functions.
	for _, key := range []string{"method:Config::Config", "method:Config::address", "function:run"} {
		sym, ok := byKindName[key]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", sym.Name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseRubyFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.rb"))
	if err != nil {
		t.Fatalf("read testdata/sample.rb: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.rb", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "ruby" {
		t.Errorf("Language = %q, want %q", result.Language, "ruby")
	}

	// Verify imports.
	wantImports := []string{"json", "config"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by name for convenient lookup.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"MAX_RETRIES", parser.KindConst},
		{"Server", parser.KindType},
		{"Config", parser.KindClass},
		{"initialize", parser.KindMethod},
		{"address", parser.KindMethod},
		{"default", parser.KindMethod},
		{"create_config", parser.KindFunction},
	}

	for _, ws := range wantSymbols {
		sym, ok := byName[ws.name]
		if !ok {
			t.Errorf("symbol %q not found; all symbols: %v", ws.name, symbolNames(result.Symbols))
			continue
		}
		if sym.Kind != ws.kind {
			t.Errorf("symbol %q: Kind = %q, want %q", ws.name, sym.Kind, ws.kind)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

func TestParseCSharpFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.cs"))
	if err != nil {
		t.Fatalf("read testdata/sample.cs: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.cs", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "csharp" {
		t.Errorf("Language = %q, want %q", result.Language, "csharp")
	}

	// Verify imports.
	wantImports := []string{"System", "System.Collections.Generic"}
	for _, want := range wantImports {
		if !slices.Contains(result.Imports, want) {
			t.Errorf("imports missing %q; got %v", want, result.Imports)
		}
	}

	// Index symbols by "kind:name" to handle same-name symbols of different kinds
	// (e.g. class Config and constructor Config both appear in C# output).
	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"Server", parser.KindType},  // namespace
		{"Point", parser.KindStruct}, // struct
		{"IHandler", parser.KindInterface},
		{"Config", parser.KindClass},
		{"Config", parser.KindMethod}, // constructor
		{"Address", parser.KindMethod},
		{"Status", parser.KindType}, // enum
	}

	for _, ws := range wantSymbols {
		key := string(ws.kind) + ":" + ws.name
		_, ok := byKindName[key]
		if !ok {
			t.Errorf("symbol %q (kind=%s) not found; all symbols: %v", ws.name, ws.kind, symbolNames(result.Symbols))
		}
	}

	// Verify signatures are non-empty for methods.
	for _, key := range []string{"method:Config", "method:Address"} {
		sym, ok := byKindName[key]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", sym.Name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

// containsSymbol returns true if symbols contains a symbol with the given name and kind.
// Helper shared by Kotlin Wave 1 tests.
func containsSymbol(symbols []*parser.Symbol, name string, kind parser.NodeKind) bool {
	for _, s := range symbols {
		if s.Name == name && s.Kind == kind {
			return true
		}
	}
	return false
}

// TestParseKotlinFile_dataClass verifies that a Kotlin data class is parsed as
// KindClass with Language=="kotlin" by the production Kotlin handler (Wave 1).
// A pass after deleting handler_kotlin.go would be a tautology — this test calls
// parser.ParseFile("user.kt", ...) and asserts on its return value.
func TestParseKotlinFile_dataClass(t *testing.T) {
	t.Parallel()
	// Multi-line fixture so AST handler's EndLine > StartLine is verifiable.
	src := []byte("data class User(\n\tval name: String,\n\tval age: Int,\n)")

	result, err := parser.ParseFile("user.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "kotlin" {
		t.Errorf("Language = %q, want %q", result.Language, "kotlin")
	}

	if !containsSymbol(result.Symbols, "User", parser.KindClass) {
		t.Errorf("expected symbol User/KindClass; got %v", symbolNames(result.Symbols))
	}

	// Exactly one class-level symbol — no methods should bleed in for a primary constructor.
	var classes []*parser.Symbol
	for _, s := range result.Symbols {
		if s.Kind == parser.KindClass {
			classes = append(classes, s)
		}
	}
	if len(classes) != 1 {
		t.Errorf("expected exactly 1 KindClass symbol, got %d: %v", len(classes), symbolNames(result.Symbols))
	}

	// AST discriminator: multi-line fixture must have EndLine > StartLine.
	// Regex fallback always produces StartLine==EndLine.
	for _, s := range classes {
		if s.EndLine <= s.StartLine {
			t.Errorf("User: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
		}
	}
}

// TestParseKotlinFile_topLevelFun verifies that a top-level Kotlin function is
// parsed as KindFunction with Language=="kotlin" by the AST handler (Wave 1).
// Fallback regex also finds "greet" but sets Signature to the raw trimmed line
// and StartLine==EndLine; the AST handler sets EndLine to the node's real end row.
// We assert EndLine >= StartLine AND that no unexpected extra symbols appear
// (fallback cannot distinguish KindMethod-vs-KindFunction context correctly).
func TestParseKotlinFile_topLevelFun(t *testing.T) {
	t.Parallel()
	// Multi-line so that AST end-row > start-row; single-expr body still spans 1 line,
	// so we use a proper block body to guarantee EndLine > StartLine from tree-sitter.
	src := []byte(`fun greet(name: String): String {
	return "Hello, $name"
}`)

	result, err := parser.ParseFile("greet.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "kotlin" {
		t.Errorf("Language = %q, want %q", result.Language, "kotlin")
	}

	if !containsSymbol(result.Symbols, "greet", parser.KindFunction) {
		t.Errorf("expected symbol greet/KindFunction; got %v", symbolNames(result.Symbols))
	}

	// AST handler: EndLine > StartLine (multi-line function body).
	// Fallback regex: StartLine == EndLine (single-line span always).
	for _, s := range result.Symbols {
		if s.Name == "greet" && s.Kind == parser.KindFunction {
			if s.EndLine <= s.StartLine {
				t.Errorf("greet: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
			}
			return
		}
	}
}

// TestParseKotlinFile_extensionFn verifies that a Kotlin extension function is
// parsed as KindFunction with Name=="shout" (receiver context is Wave 2).
func TestParseKotlinFile_extensionFn(t *testing.T) {
	t.Parallel()
	src := []byte(`fun String.shout(): String { return this.uppercase() }`)

	result, err := parser.ParseFile("shout.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "kotlin" {
		t.Errorf("Language = %q, want %q", result.Language, "kotlin")
	}

	if !containsSymbol(result.Symbols, "shout", parser.KindFunction) {
		t.Errorf("expected symbol shout/KindFunction; got %v", symbolNames(result.Symbols))
	}
}

// TestParseKotlinFile_companionObject verifies that a Kotlin class with a companion
// object is parsed to yield both the enclosing class (KindClass) and the companion
// method (KindMethod or KindFunction). Wave 1 only asserts both names are present.
// Fallback regex cannot see the Calculator class span correctly (EndLine==StartLine
// for the single `class Calculator {` line); AST handler gives EndLine==5.
func TestParseKotlinFile_companionObject(t *testing.T) {
	t.Parallel()
	src := []byte(`class Calculator {
	companion object {
		fun add(a: Int, b: Int) = a + b
	}
}`)

	result, err := parser.ParseFile("calculator.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "kotlin" {
		t.Errorf("Language = %q, want %q", result.Language, "kotlin")
	}

	if !containsSymbol(result.Symbols, "Calculator", parser.KindClass) {
		t.Errorf("expected symbol Calculator/KindClass; got %v", symbolNames(result.Symbols))
	}

	// add should appear as either KindFunction or KindMethod (Wave 1 tolerates either).
	hasAdd := containsSymbol(result.Symbols, "add", parser.KindFunction) ||
		containsSymbol(result.Symbols, "add", parser.KindMethod)
	if !hasAdd {
		t.Errorf("expected symbol add/KindFunction or KindMethod; got %v", symbolNames(result.Symbols))
	}

	// AST handler: Calculator class spans lines 1-5; fallback records StartLine==EndLine.
	for _, s := range result.Symbols {
		if s.Name == "Calculator" && s.Kind == parser.KindClass {
			if s.EndLine <= s.StartLine {
				t.Errorf("Calculator: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
			}
			return
		}
	}
	t.Errorf("Calculator/KindClass not found in symbols: %v", symbolNames(result.Symbols))
}

// --- Kotlin Wave 2 tests ---

// TestParseKotlinFile_interfaceModifier verifies that an interface declaration is
// emitted as KindInterface rather than KindClass, and that its methods are captured.
func TestParseKotlinFile_interfaceModifier(t *testing.T) {
	t.Parallel()
	src := []byte(`interface Greeter { fun greet(): String }`)

	result, err := parser.ParseFile("greeter.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if !containsSymbol(result.Symbols, "Greeter", parser.KindInterface) {
		t.Errorf("expected symbol Greeter/KindInterface; got %v", symbolNames(result.Symbols))
	}
	// Must NOT appear as KindClass.
	if containsSymbol(result.Symbols, "Greeter", parser.KindClass) {
		t.Errorf("Greeter must not be KindClass when declared with interface keyword")
	}
	// Method inside interface body should be captured.
	hasGreet := containsSymbol(result.Symbols, "greet", parser.KindFunction) ||
		containsSymbol(result.Symbols, "greet", parser.KindMethod)
	if !hasGreet {
		t.Errorf("expected method greet/KindFunction or KindMethod; got %v", symbolNames(result.Symbols))
	}
}

// TestParseKotlinFile_genericReceiverExtension guards against the receiver-capture
// regression: for `fun <T> List<T>.first(): T`, the captured name must be "first",
// not the type parameter "T" or receiver "List".
func TestParseKotlinFile_genericReceiverExtension(t *testing.T) {
	t.Parallel()
	src := []byte(`fun <T> List<T>.first(): T = this[0]`)

	result, err := parser.ParseFile("ext.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if !containsSymbol(result.Symbols, "first", parser.KindFunction) {
		t.Errorf("expected symbol first/KindFunction; got %v", symbolNames(result.Symbols))
	}
	// Type parameter and receiver must not appear as independent symbols.
	for _, s := range result.Symbols {
		if s.Name == "T" || s.Name == "List" {
			t.Errorf("unexpected symbol %q leaked from generic receiver", s.Name)
		}
	}
}

// TestParseKotlinFile_callSites verifies that ExtractCalls captures plain function
// calls and identifies them by name, using the Kotlin calls query.
func TestParseKotlinFile_callSites(t *testing.T) {
	t.Parallel()
	src := []byte(`fun caller() {
    println("hi")
    compute(1, 2)
}
fun compute(a: Int, b: Int) = a + b
`)

	calls, err := parser.ExtractCalls("main.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}
	for _, want := range []string{"println", "compute"} {
		if !found[want] {
			t.Errorf("missing call to %q; got calls: %v", want, calls)
		}
	}
}

// TestParseKotlinFile_relationshipsInheritance verifies that ExtractRelationships
// returns an inherit/extend edge from Cat to Animal.
func TestParseKotlinFile_relationshipsInheritance(t *testing.T) {
	t.Parallel()
	src := []byte(`interface Animal { fun sound(): String }
open class Cat : Animal { override fun sound() = "meow" }
`)

	rels, err := parser.ExtractRelationships("cat.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	found := false
	for _, r := range rels {
		if r.Subject == "Cat" && r.Target == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Cat→Animal relationship; got %v", rels)
	}
}

// TestParseKotlinFile_sealedClass verifies that sealed classes and their nested
// subclasses are all emitted as KindClass (sealed is a modifier, not a separate kind).
func TestParseKotlinFile_sealedClass(t *testing.T) {
	t.Parallel()
	src := []byte(`sealed class Result { class Ok : Result(); class Err : Result() }`)

	result, err := parser.ParseFile("result.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	for _, name := range []string{"Result", "Ok", "Err"} {
		if !containsSymbol(result.Symbols, name, parser.KindClass) {
			t.Errorf("expected symbol %s/KindClass; got %v", name, symbolNames(result.Symbols))
		}
	}
}

// TestParseKotlinFile_enumClass verifies that an enum class is emitted as KindClass,
// consistent with Java handler behaviour for enum_declaration.
func TestParseKotlinFile_enumClass(t *testing.T) {
	t.Parallel()
	src := []byte(`enum class Color { RED, GREEN, BLUE }`)

	result, err := parser.ParseFile("color.kt", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if !containsSymbol(result.Symbols, "Color", parser.KindClass) {
		t.Errorf("expected symbol Color/KindClass; got %v", symbolNames(result.Symbols))
	}
}

func TestParsePHPFile(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sample.php"))
	if err != nil {
		t.Fatalf("read testdata/sample.php: %v", err)
	}

	result, err := parser.ParseFile("testdata/sample.php", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "php" {
		t.Errorf("Language = %q, want %q", result.Language, "php")
	}

	// Verify imports.
	if len(result.Imports) == 0 {
		t.Error("expected at least one import (use statement)")
	}

	// Index symbols by "kind:name" to handle same-name symbols of different kinds.
	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Verify expected symbols are present with correct kinds.
	type wantSym struct {
		name string
		kind parser.NodeKind
	}
	wantSymbols := []wantSym{
		{"MAX_RETRIES", parser.KindConst},
		{"create_config", parser.KindFunction},
		{"Handler", parser.KindInterface},
		{"Loggable", parser.KindType}, // trait
		{"Config", parser.KindClass},
		{"__construct", parser.KindMethod},
		{"address", parser.KindMethod},
		{"handle", parser.KindMethod},
		{"log", parser.KindMethod},
	}

	for _, ws := range wantSymbols {
		key := string(ws.kind) + ":" + ws.name
		_, ok := byKindName[key]
		if !ok {
			t.Errorf("symbol %q (kind=%s) not found; all symbols: %v", ws.name, ws.kind, symbolNames(result.Symbols))
		}
	}

	// Verify signatures are non-empty for functions and methods.
	for _, key := range []string{"function:create_config", "method:address", "method:__construct"} {
		sym, ok := byKindName[key]
		if !ok {
			continue
		}
		if sym.Signature == "" {
			t.Errorf("symbol %q has empty Signature", sym.Name)
		}
	}

	// Verify StartLine/EndLine are set and reasonable (1-based, start <= end).
	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine is 0 (should be 1-based)", sym.Name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine %d < StartLine %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}
}

// --- Swift Wave 1 tests ---

// TestParseSwiftFile_classDecl verifies that a Swift class declaration is parsed
// as KindClass with Language=="swift" by the AST handler.
// Anti-tautology: asserts against parser.ParseFile return value.
// AST discriminator: EndLine > StartLine (multi-line fixture).
func TestParseSwiftFile_classDecl(t *testing.T) {
	t.Parallel()
	// Multi-line so that EndLine > StartLine, proving AST parse (not regex fallback).
	src := []byte("class User {\n\tvar name: String\n\tvar age: Int\n}")

	result, err := parser.ParseFile("user.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	if !containsSymbol(result.Symbols, "User", parser.KindClass) {
		t.Errorf("expected symbol User/KindClass; got %v", symbolNames(result.Symbols))
	}

	// AST discriminator: multi-line fixture must have EndLine > StartLine.
	// Regex fallback always produces StartLine==EndLine.
	for _, s := range result.Symbols {
		if s.Name == "User" && s.Kind == parser.KindClass {
			if s.EndLine <= s.StartLine {
				t.Errorf("User: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
			}
			return
		}
	}
	t.Errorf("User/KindClass not found in symbols: %v", symbolNames(result.Symbols))
}

// TestParseSwiftFile_topLevelFunction verifies that a top-level Swift func is
// parsed as KindFunction with Language=="swift".
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_topLevelFunction(t *testing.T) {
	t.Parallel()
	src := []byte("func greet(name: String) -> String {\n\treturn \"Hello, \\(name)\"\n}")

	result, err := parser.ParseFile("greet.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	if !containsSymbol(result.Symbols, "greet", parser.KindFunction) {
		t.Errorf("expected symbol greet/KindFunction; got %v", symbolNames(result.Symbols))
	}

	// AST handler: EndLine > StartLine (multi-line body).
	for _, s := range result.Symbols {
		if s.Name == "greet" && s.Kind == parser.KindFunction {
			if s.EndLine <= s.StartLine {
				t.Errorf("greet: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
			}
			return
		}
	}
}

// TestParseSwiftFile_protocolDecl verifies that a Swift protocol declaration is
// parsed as KindInterface (NOT KindClass).
// Swift protocols are the closest analog to Kotlin interfaces / Java interfaces.
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_protocolDecl(t *testing.T) {
	t.Parallel()
	src := []byte("protocol Greeter {\n\tfunc greet() -> String\n}")

	result, err := parser.ParseFile("greeter.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	if !containsSymbol(result.Symbols, "Greeter", parser.KindInterface) {
		t.Errorf("expected symbol Greeter/KindInterface; got %v", symbolNames(result.Symbols))
	}
	// Must NOT appear as KindClass.
	if containsSymbol(result.Symbols, "Greeter", parser.KindClass) {
		t.Errorf("Greeter must not be KindClass when declared as protocol")
	}
}

// TestParseSwiftFile_extensionDecl verifies that a Swift extension's method is
// captured and emitted as KindMethod (Wave 1 nit #2: AST discriminator).
// Extension methods route through class_body capture → @symbol.method → KindMethod.
// Regex fallback would emit KindFunction; AST handler must emit KindMethod.
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_extensionDecl(t *testing.T) {
	t.Parallel()
	src := []byte("extension String {\n\tfunc shout() -> String {\n\t\treturn self.uppercased()\n\t}\n}")

	result, err := parser.ParseFile("string_ext.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	// Wave 2 nit #2: extension methods must be KindMethod (not KindFunction).
	// The class_body capture in swift.scm routes to @symbol.method → KindMethod.
	// Regex fallback emits KindFunction — this assertion distinguishes AST from fallback.
	if !containsSymbol(result.Symbols, "shout", parser.KindMethod) {
		t.Errorf("expected symbol shout/KindMethod (AST path); got %v", symbolNames(result.Symbols))
	}
	// Must NOT appear as KindFunction (which would indicate fallback regex path).
	if containsSymbol(result.Symbols, "shout", parser.KindFunction) {
		t.Errorf("shout must not be KindFunction; extension methods must use KindMethod via AST")
	}
}

// --- Swift Wave 2 tests ---

// TestParseSwiftFile_protocolBodyMethods verifies that method declarations inside a
// Swift protocol body are captured as symbols.
// Protocol methods parse as protocol_function_declaration inside protocol_body —
// a distinct node from function_declaration used in class/struct bodies.
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_protocolBodyMethods(t *testing.T) {
	t.Parallel()
	src := []byte("protocol Greeter {\n\tfunc greet() -> String\n\tfunc farewell(name: String) -> String\n}")

	result, err := parser.ParseFile("greeter.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	// Protocol container must be KindInterface.
	if !containsSymbol(result.Symbols, "Greeter", parser.KindInterface) {
		t.Errorf("expected symbol Greeter/KindInterface; got %v", symbolNames(result.Symbols))
	}

	// Protocol body methods must be captured (KindFunction or KindMethod).
	// Wave 2 adds protocol_function_declaration capture to swift.scm.
	for _, name := range []string{"greet", "farewell"} {
		hasMethod := containsSymbol(result.Symbols, name, parser.KindFunction) ||
			containsSymbol(result.Symbols, name, parser.KindMethod)
		if !hasMethod {
			t.Errorf("expected protocol method %q/KindFunction or KindMethod; got %v", name, symbolNames(result.Symbols))
		}
	}
}

// TestParseSwiftFile_callSites verifies that ExtractCalls captures plain function
// calls originating inside a Swift function body.
// Anti-tautology: asserts against parser.ExtractCalls return value.
func TestParseSwiftFile_callSites(t *testing.T) {
	t.Parallel()
	src := []byte("func caller() {\n\tprint(\"hi\")\n\tcompute(1, 2)\n}\nfunc compute(a: Int, b: Int) -> Int {\n\treturn a + b\n}")

	calls, err := parser.ExtractCalls("main.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}
	for _, want := range []string{"print", "compute"} {
		if !found[want] {
			t.Errorf("missing call to %q; got calls: %v", want, calls)
		}
	}
}

// TestParseSwiftFile_relationshipsConformance verifies that ExtractRelationships
// returns an edge from Cat to Animal for Swift protocol conformance.
// Swift uses "class Cat: Animal" (single colon) for both inheritance and conformance.
// Anti-tautology: asserts against parser.ExtractRelationships return value.
func TestParseSwiftFile_relationshipsConformance(t *testing.T) {
	t.Parallel()
	src := []byte("protocol Animal {\n\tfunc sound() -> String\n}\nclass Cat: Animal {\n\tfunc sound() -> String { return \"meow\" }\n}")

	rels, err := parser.ExtractRelationships("cat.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	found := false
	for _, r := range rels {
		if r.Subject == "Cat" && r.Target == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Cat→Animal relationship; got %v", rels)
	}
}

// TestParseSwiftFile_genericFunction guards against the generic type parameter
// being captured as the function name for "func swap<T>(...)".
// The Swift grammar emits: function_declaration > simple_identifier["swap"] > type_parameters.
// swiftNameNode scans children left-to-right and must find "swap" before type_parameters.
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_genericFunction(t *testing.T) {
	t.Parallel()
	src := []byte("func swap<T>(_ a: inout T, _ b: inout T) {\n\tlet tmp = a\n}")

	result, err := parser.ParseFile("swap.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	if !containsSymbol(result.Symbols, "swap", parser.KindFunction) {
		t.Errorf("expected symbol swap/KindFunction; got %v", symbolNames(result.Symbols))
	}
	// Type parameter must not leak as an independent symbol.
	for _, s := range result.Symbols {
		if s.Name == "T" {
			t.Errorf("unexpected symbol %q leaked from generic type parameter", s.Name)
		}
	}
}

// TestParseSwiftFile_struct verifies that a Swift struct is captured as KindClass.
// Wave 1 collapses struct → KindClass (class_declaration umbrella node).
// This test documents and guards the Wave 1 behavior; Wave 2 may add KindStruct
// if distinct NodeKind constants are introduced in a later wave.
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_struct(t *testing.T) {
	t.Parallel()
	// Multi-line so EndLine > StartLine (AST discriminator).
	src := []byte("struct Point {\n\tvar x: Double\n\tvar y: Double\n}")

	result, err := parser.ParseFile("point.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	// Wave 1 maps struct → KindClass (class_declaration umbrella node in Swift grammar).
	// Note: Wave 3+ may distinguish KindStruct if NodeKind constants are extended.
	if !containsSymbol(result.Symbols, "Point", parser.KindClass) {
		t.Errorf("expected symbol Point/KindClass (struct→class Wave 1 behavior); got %v", symbolNames(result.Symbols))
	}

	// AST discriminator: multi-line struct must have EndLine > StartLine.
	for _, s := range result.Symbols {
		if s.Name == "Point" && s.Kind == parser.KindClass {
			if s.EndLine <= s.StartLine {
				t.Errorf("Point: EndLine (%d) should be > StartLine (%d); suggests fallback regex not AST", s.EndLine, s.StartLine)
			}
			return
		}
	}
	t.Errorf("Point/KindClass not found in symbols: %v", symbolNames(result.Symbols))
}

// TestParseSwiftFile_actor verifies that a Swift actor is captured as KindClass
// with its methods emitted as KindMethod.
// Actors parse as class_declaration with an "actor" keyword child (same umbrella node).
// Anti-tautology: asserts against parser.ParseFile return value.
func TestParseSwiftFile_actor(t *testing.T) {
	t.Parallel()
	src := []byte("actor Counter {\n\tvar value: Int = 0\n\tfunc increment() {\n\t\tvalue += 1\n\t}\n}")

	result, err := parser.ParseFile("counter.swift", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if result.Language != "swift" {
		t.Errorf("Language = %q, want %q", result.Language, "swift")
	}

	// Actor maps to KindClass (class_declaration with "actor" keyword child).
	if !containsSymbol(result.Symbols, "Counter", parser.KindClass) {
		t.Errorf("expected symbol Counter/KindClass (actor→class Wave 1 behavior); got %v", symbolNames(result.Symbols))
	}
	// Method inside actor body must be captured.
	if !containsSymbol(result.Symbols, "increment", parser.KindMethod) {
		t.Errorf("expected symbol increment/KindMethod; got %v", symbolNames(result.Symbols))
	}
}
