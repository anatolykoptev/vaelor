package parser_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

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
