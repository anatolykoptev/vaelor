package codegraph

import "github.com/anatolykoptev/go-code/internal/parser"

// symbolComplexity estimates cyclomatic complexity from a function body.
// Delegates to parser.Complexity. Returns 1 for empty body (graph convention).
func symbolComplexity(body string) int {
	cc := parser.Complexity(body)
	if cc == 0 {
		return 1 // graph convention: empty body → complexity 1
	}
	return cc
}
