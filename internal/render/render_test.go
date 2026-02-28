package render

import (
	"strings"
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

	// Function signatures should be present (clean, without opening brace).
	assertContains(t, result, "func Hello(name string) string")
	assertContains(t, result, "func Goodbye(name string) string")

	// Function bodies should be removed.
	assertNotContains(t, result, "greeting :=")
	assertNotContains(t, result, "return greeting")
	assertNotContains(t, result, `return fmt.Sprintf("Goodbye`)

	// No dangling opening braces from function declarations.
	assertNotContains(t, result, "string {")

	// No closing braces from function bodies (struct closing brace is OK).
	// Count braces: only struct should have them.
	lines := strings.Split(result, "\n")
	braceCount := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "}" {
			braceCount++
		}
	}
	if braceCount != 1 { // only the struct closing brace
		t.Errorf("expected 1 closing brace (struct), got %d\nresult:\n%s", braceCount, result)
	}
}

func TestRenderFile_Skeleton(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeSkeleton})

	// Struct body should be preserved.
	assertContains(t, result, "Name    string")

	// Function opening lines should be present (with opening brace).
	assertContains(t, result, "func Hello(name string) string {")
	assertContains(t, result, "func Goodbye(name string) string {")

	// Placeholder should appear for function bodies.
	assertContains(t, result, "// ...")

	// Closing braces should be preserved.
	lines := strings.Split(result, "\n")
	braceCount := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "}" {
			braceCount++
		}
	}
	// struct + 2 functions = 3 closing braces
	if braceCount != 3 { //nolint:mnd // struct + 2 functions
		t.Errorf("expected 3 closing braces, got %d\nresult:\n%s", braceCount, result)
	}

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

	// Goodbye is not relevant — body should be stripped, signature only.
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

func TestRenderFile_Focused_MatchesSignature(t *testing.T) {
	// "string" appears in the signature but not the function name.
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{
		Mode:       ModeFocused,
		QueryTerms: []string{"string"},
	})

	// Both functions have "string" in their signature — both should be kept.
	assertContains(t, result, "greeting :=")
	assertContains(t, result, `return fmt.Sprintf("Goodbye`)
}

func TestRenderFile_Focused_EmptyQueryTerms(t *testing.T) {
	// Empty terms means nothing is relevant — acts like signatures mode.
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{
		Mode:       ModeFocused,
		QueryTerms: nil,
	})

	assertNotContains(t, result, "greeting :=")
	assertContains(t, result, "func Hello(name string) string")
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

func TestRenderFile_EmptySource(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "X", Kind: parser.KindFunction, StartLine: 1, EndLine: 3, Signature: "func X()"},
	}

	result := RenderFile("", symbols, Opts{Mode: ModeSignatures})
	if result != "" {
		t.Errorf("empty source should return empty\ngot:\n%q", result)
	}
}

func TestRenderFile_NestedSymbols(t *testing.T) {
	// Python-style: class contains methods. Class is structural (kept),
	// methods should have bodies stripped in signatures mode.
	source := `class MyClass:
    x = 1

    def method(self):
        do_stuff()
        return True

    def other(self):
        return False
`
	symbols := []*parser.Symbol{
		{Name: "MyClass", Kind: parser.KindClass, StartLine: 1, EndLine: 9},
		{Name: "method", Kind: parser.KindMethod, StartLine: 4, EndLine: 6, Signature: "def method(self)"},
		{Name: "other", Kind: parser.KindMethod, StartLine: 8, EndLine: 9, Signature: "def other(self)"},
	}

	result := RenderFile(source, symbols, Opts{Mode: ModeSignatures})

	// Class line should remain.
	assertContains(t, result, "class MyClass:")
	// Class field should remain (not inside a method).
	assertContains(t, result, "x = 1")
	// Method signatures should be present.
	assertContains(t, result, "def method(self)")
	assertContains(t, result, "def other(self)")
	// Method bodies should be stripped.
	assertNotContains(t, result, "do_stuff()")
	assertNotContains(t, result, "return True")
	assertNotContains(t, result, "return False")
}

func TestRenderFile_NestedFunctions(t *testing.T) {
	// Nested functions: outer contains inner. Both are non-structural.
	// In signatures mode, outer replacement should cover inner.
	source := `def outer():
    def inner():
        return 1
    inner()
    return 2
`
	symbols := []*parser.Symbol{
		{Name: "outer", Kind: parser.KindFunction, StartLine: 1, EndLine: 5, Signature: "def outer()"},
		{Name: "inner", Kind: parser.KindFunction, StartLine: 2, EndLine: 3, Signature: "def inner()"},
	}

	result := RenderFile(source, symbols, Opts{Mode: ModeSignatures})

	// Only outer signature should appear. Inner is fully nested in outer's
	// body — it's covered by the outer replacement.
	assertContains(t, result, "def outer()")
	assertNotContains(t, result, "def inner()")
	assertNotContains(t, result, "return 1")
	assertNotContains(t, result, "inner()")
}

func TestRenderFile_NestedFunctions_Skeleton(t *testing.T) {
	source := `def outer():
    def inner():
        return 1
    inner()
    return 2
`
	symbols := []*parser.Symbol{
		{Name: "outer", Kind: parser.KindFunction, StartLine: 1, EndLine: 5, Signature: "def outer()"},
		{Name: "inner", Kind: parser.KindFunction, StartLine: 2, EndLine: 3, Signature: "def inner()"},
	}

	result := RenderFile(source, symbols, Opts{Mode: ModeSkeleton})

	// Outer opening and closing lines kept, body replaced with placeholder.
	assertContains(t, result, "def outer():")
	assertContains(t, result, "// ...")
	// Inner function body should not leak through.
	assertNotContains(t, result, "return 1")
}

func TestRenderFile_InvalidMode(t *testing.T) {
	// Invalid mode is treated as default — source returned unchanged.
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: "typo"})
	if result != sampleGoSource {
		t.Errorf("invalid mode should return source unchanged\ngot:\n%s", result)
	}
}

func TestValidMode(t *testing.T) {
	valid := []string{"", "signatures", "skeleton", "focused"}
	for _, m := range valid {
		if !ValidMode(m) {
			t.Errorf("ValidMode(%q) = false, want true", m)
		}
	}

	invalid := []string{"typo", "signtures", "Signatures", "SKELETON"}
	for _, m := range invalid {
		if ValidMode(m) {
			t.Errorf("ValidMode(%q) = true, want false", m)
		}
	}
}

func TestIsRelevant(t *testing.T) {
	sym := &parser.Symbol{Name: "ParseFile", Signature: "func ParseFile(path string) error"}

	tests := []struct {
		terms []string
		want  bool
	}{
		{[]string{"parse"}, true},
		{[]string{"file"}, true},
		{[]string{"parsefile"}, true},
		{[]string{"other"}, false},
		{nil, false},
		// Matches against signature too.
		{[]string{"path"}, true},
		{[]string{"error"}, true},
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

func TestRemoveNested(t *testing.T) {
	input := []replacement{
		{startLine: 1, endLine: 10, action: actionSignatures},
		{startLine: 3, endLine: 5, action: actionSignatures},  // nested in first
		{startLine: 7, endLine: 9, action: actionSignatures},  // nested in first
		{startLine: 15, endLine: 20, action: actionSignatures}, // independent
	}

	result := removeNested(input)
	if len(result) != 2 { //nolint:mnd // expected 2 non-nested
		t.Fatalf("expected 2 replacements, got %d: %+v", len(result), result)
	}
	if result[0].startLine != 1 || result[1].startLine != 15 {
		t.Errorf("expected startLines [1, 15], got [%d, %d]", result[0].startLine, result[1].startLine)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q\ngot:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected output to NOT contain %q\ngot:\n%s", substr, s)
	}
}
