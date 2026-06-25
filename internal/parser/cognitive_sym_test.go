package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestCognitiveComplexityPopulated verifies that Symbol.CognitiveComplexity is
// populated by ParseFile. Run BEFORE the fix to confirm it returns 0 (RED),
// then again after (GREEN).
func TestCognitiveComplexityPopulated(t *testing.T) {
	// Flat function: 5 sequential if-branches, no nesting.
	// Cyclomatic complexity = 6 (5 branches + 1), cognitive >= 5.
	flatCode := []byte(`package flat

func flatFunc(a, b, c, d, e int) int {
	if a > 0 { return 1 }
	if b > 0 { return 2 }
	if c > 0 { return 3 }
	if d > 0 { return 4 }
	if e > 0 { return 5 }
	return 0
}
`)

	result, err := parser.ParseFile("flat.go", flatCode, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var found *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "flatFunc" {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatal("flatFunc symbol not found in parsed output")
	}

	if found.CognitiveComplexity <= 0 {
		t.Errorf("want CognitiveComplexity > 0 for flatFunc, got %d (Complexity=%d)",
			found.CognitiveComplexity, found.Complexity)
	}
	t.Logf("flatFunc: cyclomatic=%d cognitive=%d", found.Complexity, found.CognitiveComplexity)
}
