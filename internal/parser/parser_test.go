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

// symbolNames returns just the names for error messages.
func symbolNames(syms []*parser.Symbol) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}
