package parser_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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
		{"module.cjs", "javascript"},
		{"module.cts", "typescript"},
		{"module.mts", "typescript"},
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

func TestParseFileAliases(t *testing.T) {
	src := []byte("function hello() { return 42; }\n")
	cases := []struct {
		ext      string
		wantLang string
	}{
		{".mjs", "javascript"},
		{".cjs", "javascript"},
		{".cts", "typescript"},
		{".mts", "typescript"},
		{".js", "javascript"},
		{".ts", "typescript"},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			result, err := parser.ParseFile("module"+tc.ext, src, parser.ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): unexpected error: %v", tc.ext, err)
			}
			if len(result.Symbols) == 0 {
				t.Errorf("ParseFile(%q): expected at least one symbol, got none", tc.ext)
			}
			if result.Language != tc.wantLang {
				t.Errorf("ParseFile(%q): Language = %q, want %q", tc.ext, result.Language, tc.wantLang)
			}
		})
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

func TestParsePHPFile(t *testing.T) {
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

func TestParseGoFile_NoLocalVars(t *testing.T) {
	source := []byte(`package example

import "strings"

// Version is a package-level const.
const Version = "1.0"

// DefaultName is a package-level var.
var DefaultName = "example"

func process(input string) string {
	// These should NOT appear in symbols.
	var sb strings.Builder
	const prefix = "test"
	sb.WriteString(prefix + input)
	return sb.String()
}
`)

	result, err := parser.ParseFile("example.go", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Name == "sb" || sym.Name == "prefix" {
			t.Errorf("local %s %q should not appear in symbols", sym.Kind, sym.Name)
		}
	}

	// Verify package-level declarations ARE captured.
	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}
	if _, ok := byName["Version"]; !ok {
		t.Error("package-level const Version should be captured")
	}
	if _, ok := byName["DefaultName"]; !ok {
		t.Error("package-level var DefaultName should be captured")
	}
	if _, ok := byName["process"]; !ok {
		t.Error("function process should be captured")
	}
}

func TestParseGoFile_ConstBlockSignature(t *testing.T) {
	source := []byte(`package example

const (
	Foo = "foo"
	Bar = "bar"
)
`)

	result, err := parser.ParseFile("example.go", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Kind == parser.KindConst && strings.HasPrefix(sym.Signature, "const (") {
			t.Errorf("const %q signature should not start with 'const (', got: %q", sym.Name, sym.Signature)
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

func TestDocCommentExtraction(t *testing.T) {
	source := []byte(`package sample

// Exported is a documented function.
func Exported() {}

func NoDoc() {}

// MultiLine is documented
// with multiple lines.
func MultiLine() {}

// DocType is a documented type.
type DocType struct{}
`)

	result, err := parser.ParseFile("doc_test.go", source, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	if sym, ok := byName["Exported"]; !ok {
		t.Error("Exported symbol not found")
	} else if sym.DocComment == "" {
		t.Error("Exported should have a doc comment")
	} else if !strings.Contains(sym.DocComment, "documented function") {
		t.Errorf("Exported doc = %q, want to contain 'documented function'", sym.DocComment)
	}

	if sym, ok := byName["NoDoc"]; !ok {
		t.Error("NoDoc symbol not found")
	} else if sym.DocComment != "" {
		t.Errorf("NoDoc should have no doc comment, got %q", sym.DocComment)
	}

	if sym, ok := byName["MultiLine"]; !ok {
		t.Error("MultiLine symbol not found")
	} else if !strings.Contains(sym.DocComment, "multiple lines") {
		t.Errorf("MultiLine doc = %q, want to contain 'multiple lines'", sym.DocComment)
	}

	if sym, ok := byName["DocType"]; !ok {
		t.Error("DocType symbol not found")
	} else if sym.DocComment == "" {
		t.Error("DocType should have a doc comment")
	}
}
