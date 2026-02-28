package render

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// sampleGoSource is a minimal Go file with two functions and a struct.
const sampleGoSource = `package example

import "fmt"

type Config struct {
	Name    string
	Timeout int
}

func Hello(name string) string {
	greeting := fmt.Sprintf("Hello, %s!", name)
	return greeting
}

func Goodbye(name string) string {
	return fmt.Sprintf("Goodbye, %s!", name)
}
`

// sampleSymbols mirrors the symbols that tree-sitter would extract from sampleGoSource.
var sampleSymbols = []*parser.Symbol{
	{
		Name:      "Config",
		Kind:      parser.KindStruct,
		StartLine: 5,
		EndLine:   8,
		Signature: "type Config struct",
	},
	{
		Name:      "Hello",
		Kind:      parser.KindFunction,
		StartLine: 10,
		EndLine:   13,
		Signature: "func Hello(name string) string",
	},
	{
		Name:      "Goodbye",
		Kind:      parser.KindFunction,
		StartLine: 15,
		EndLine:   17,
		Signature: "func Goodbye(name string) string",
	},
}

func TestRenderFile_DefaultMode(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeDefault})
	if result != sampleGoSource {
		t.Errorf("default mode should return source unchanged\ngot:\n%s", result)
	}
}

func TestRenderFile_NoSymbols(t *testing.T) {
	result := RenderFile(sampleGoSource, nil, Opts{Mode: ModeSignatures})
	if result != sampleGoSource {
		t.Errorf("no symbols should return source unchanged\ngot:\n%s", result)
	}
}

func TestRenderFile_Signatures(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeSignatures})

	// Struct body should be preserved (structural kind).
	assertContains(t, result, "Name    string")
	assertContains(t, result, "Timeout int")

	// Function signatures should be present.
	assertContains(t, result, "func Hello(name string) string")
	assertContains(t, result, "func Goodbye(name string) string")

	// Function bodies should be removed.
	assertNotContains(t, result, "greeting :=")
	assertNotContains(t, result, "return greeting")
	assertNotContains(t, result, `return fmt.Sprintf("Goodbye`)
}

func TestRenderFile_Skeleton(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeSkeleton})

	// Struct body should be preserved.
	assertContains(t, result, "Name    string")

	// Function signatures should be present.
	assertContains(t, result, "func Hello(name string) string")
	assertContains(t, result, "func Goodbye(name string) string")

	// Placeholder should appear for function bodies.
	assertContains(t, result, "// ...")

	// Actual bodies should be removed.
	assertNotContains(t, result, "greeting :=")
	assertNotContains(t, result, `return fmt.Sprintf("Goodbye`)
}

func TestRenderFile_Focused_RelevantSymbol(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{
		Mode:       ModeFocused,
		QueryTerms: []string{"hello"},
	})

	// Hello is relevant — full body should be present.
	assertContains(t, result, "greeting :=")
	assertContains(t, result, "return greeting")

	// Goodbye is not relevant — body should be stripped.
	assertContains(t, result, "func Goodbye(name string) string")
	assertNotContains(t, result, `return fmt.Sprintf("Goodbye`)
}

func TestRenderFile_Focused_NoRelevantSymbols(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{
		Mode:       ModeFocused,
		QueryTerms: []string{"nonexistent"},
	})

	// No symbols are relevant — all function bodies should be stripped.
	assertNotContains(t, result, "greeting :=")
	assertNotContains(t, result, `return fmt.Sprintf("Goodbye`)

	// Signatures should remain.
	assertContains(t, result, "func Hello(name string) string")
	assertContains(t, result, "func Goodbye(name string) string")
}

func TestRenderFile_Focused_CaseInsensitive(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{
		Mode:       ModeFocused,
		QueryTerms: []string{"hello"}, // lowercase matches "Hello"
	})

	// Hello body should be preserved.
	assertContains(t, result, "greeting :=")
}

func TestRenderFile_SingleLineSymbol(t *testing.T) {
	source := "package main\n\nvar X = 42\n"
	symbols := []*parser.Symbol{
		{Name: "X", Kind: parser.KindVar, StartLine: 3, EndLine: 3},
	}

	// Single-line symbol (StartLine == EndLine) should not be modified.
	result := RenderFile(source, symbols, Opts{Mode: ModeSignatures})
	if result != source {
		t.Errorf("single-line symbol should be unchanged\ngot:\n%s", result)
	}
}

func TestIsRelevant(t *testing.T) {
	sym := &parser.Symbol{Name: "ParseFile"}

	tests := []struct {
		terms []string
		want  bool
	}{
		{[]string{"parse"}, true},
		{[]string{"file"}, true},
		{[]string{"parsefile"}, true},
		{[]string{"other"}, false},
		{nil, false},
	}

	for _, tt := range tests {
		got := isRelevant(sym, tt.terms)
		if got != tt.want {
			t.Errorf("isRelevant(%q, %v) = %v, want %v", sym.Name, tt.terms, got, tt.want)
		}
	}
}

func TestIsStructuralKind(t *testing.T) {
	structural := []parser.NodeKind{
		parser.KindStruct, parser.KindInterface, parser.KindClass, parser.KindType,
	}
	nonStructural := []parser.NodeKind{
		parser.KindFunction, parser.KindMethod, parser.KindConst, parser.KindVar,
		parser.KindImport, parser.KindModule,
	}

	for _, k := range structural {
		if !isStructuralKind(k) {
			t.Errorf("expected %q to be structural", k)
		}
	}
	for _, k := range nonStructural {
		if isStructuralKind(k) {
			t.Errorf("expected %q to not be structural", k)
		}
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !containsStr(s, substr) {
		t.Errorf("expected output to contain %q\ngot:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if containsStr(s, substr) {
		t.Errorf("expected output to NOT contain %q\ngot:\n%s", substr, s)
	}
}

func containsStr(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && contains(s, substr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
