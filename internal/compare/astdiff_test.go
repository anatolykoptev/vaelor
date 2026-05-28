package compare

import (
	"testing"

	"github.com/smacker/gum"
)

func TestComputeASTDiff_Modified(t *testing.T) {
	bodyA := `func Add(a int) int {
	return a
}`
	bodyB := `func Add(a int, b int) int {
	return a + b
}`

	diff := ComputeASTDiff(bodyA, bodyB, "go")
	if diff == nil {
		t.Fatal("expected non-nil DiffSummary for modified Go function")
	}
	if diff.TotalChanges == 0 {
		t.Error("expected TotalChanges > 0")
	}
	if len(diff.Changes) == 0 {
		t.Error("expected non-empty Changes")
	}

	t.Logf("diff: total=%d inserts=%d deletes=%d updates=%d moves=%d",
		diff.TotalChanges, diff.Inserts, diff.Deletes, diff.Updates, diff.Moves)
	for i, c := range diff.Changes {
		t.Logf("  change[%d]: %s", i, c)
	}
}

func TestComputeASTDiff_Identical(t *testing.T) {
	body := `func Add(a, b int) int {
	return a + b
}`

	diff := ComputeASTDiff(body, body, "go")
	if diff == nil {
		t.Fatal("expected non-nil DiffSummary for identical bodies")
	}
	if diff.TotalChanges != 0 {
		t.Errorf("expected TotalChanges=0 for identical bodies, got %d", diff.TotalChanges)
	}
}

func TestComputeASTDiff_UnsupportedLanguage(t *testing.T) {
	diff := ComputeASTDiff("func foo() {}", "func bar() {}", "brainfuck")
	if diff != nil {
		t.Error("expected nil for unsupported language")
	}
}

func TestComputeASTDiff_EmptyBody(t *testing.T) {
	diff := ComputeASTDiff("", "func foo() {}", "go")
	if diff != nil {
		t.Error("expected nil for empty bodyA")
	}

	diff = ComputeASTDiff("func foo() {}", "", "go")
	if diff != nil {
		t.Error("expected nil for empty bodyB")
	}
}

func TestComputeASTDiff_Python(t *testing.T) {
	bodyA := `def greet(name):
    return "Hello, " + name
`
	bodyB := `def greet(name, greeting="Hi"):
    return greeting + ", " + name
`

	diff := ComputeASTDiff(bodyA, bodyB, "python")
	if diff == nil {
		t.Fatal("expected non-nil DiffSummary for modified Python function")
	}
	if diff.TotalChanges == 0 {
		t.Error("expected TotalChanges > 0 for modified Python function")
	}

	t.Logf("python diff: total=%d inserts=%d deletes=%d updates=%d moves=%d",
		diff.TotalChanges, diff.Inserts, diff.Deletes, diff.Updates, diff.Moves)
	for i, c := range diff.Changes {
		t.Logf("  change[%d]: %s", i, c)
	}
}

// TestLookupLanguage_Swift verifies that lookupLanguage returns a non-nil
// tree-sitter language for "swift" (internal/compare/astdiff.go).
func TestLookupLanguage_Swift(t *testing.T) {
	lang := lookupLanguage("swift")
	if lang == nil {
		t.Fatal("lookupLanguage(\"swift\") returned nil — Swift tree-sitter grammar not registered")
	}
}

// TestLookupLanguage_Kotlin verifies that lookupLanguage returns a non-nil
// tree-sitter language for "kotlin" (internal/compare/astdiff.go).
func TestLookupLanguage_Kotlin(t *testing.T) {
	lang := lookupLanguage("kotlin")
	if lang == nil {
		t.Fatal("lookupLanguage(\"kotlin\") returned nil — Kotlin tree-sitter grammar not registered")
	}
}

func TestSummarizeActions_Empty(t *testing.T) {
	result := summarizeActions(nil, "go")
	if result != nil {
		t.Errorf("expected nil for empty actions, got %v", result)
	}

	result = summarizeActions([]*gum.Action{}, "go")
	if result != nil {
		t.Errorf("expected nil for zero-length actions, got %v", result)
	}
}
