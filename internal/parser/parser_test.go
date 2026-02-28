package parser_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestParseGoFile(t *testing.T) {
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

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"server.go", "go"},
		{"script.py", "python"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"index.js", "javascript"},
		{"module.mjs", "javascript"},
		{"main.rs", "rust"},
		{"Main.java", "java"},
		{"main.c", "c"},
		{"header.h", "c"},
		{"main.cpp", "cpp"},
		{"main.cc", "cpp"},
		{"main.cxx", "cpp"},
		{"main.hpp", "cpp"},
		{"script.rb", "ruby"},
		{"Program.cs", "csharp"},
		{"unknown.xyz", ""},
		{"no_extension", ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := parser.DetectLanguageFromPath(tc.path)
			if got != tc.want {
				t.Errorf("DetectLanguageFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseUnsupportedExtension(t *testing.T) {
	_, err := parser.ParseFile("file.unknown", []byte("content"), parser.ParseOpts{})
	if err == nil {
		t.Error("expected error for unsupported extension, got nil")
	}
}

func TestParsePythonFile(t *testing.T) {
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
		{"Config", parser.KindType},       // typedef struct { ... } Config
		{"Server", parser.KindStruct},     // struct Server { ... }
		{"Status", parser.KindType},       // enum Status
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

// symbolNames returns just the names for error messages.
func symbolNames(syms []*parser.Symbol) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}
