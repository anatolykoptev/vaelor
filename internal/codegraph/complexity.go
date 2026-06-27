package codegraph

import "github.com/anatolykoptev/go-code/internal/parser"

// symbolComplexity estimates cyclomatic complexity from a function body.
// Delegates to parser.Complexity, which is the single owner: comments are
// stripped before counting and empty bodies return 1 (graph convention).
func symbolComplexity(body string) int {
	return parser.Complexity(body, "")
}
