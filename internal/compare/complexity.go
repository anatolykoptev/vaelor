package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// cyclomaticComplexity delegates to parser.Complexity, which is the single
// owner of cyclomatic complexity calculation. Comments are stripped and
// empty bodies return 1 inside parser.Complexity.
func cyclomaticComplexity(body, language string) int {
	return parser.Complexity(body, language)
}

// cognitiveComplexity wraps parser.CognitiveComplexity for use in metrics computation.
func cognitiveComplexity(body, language string) int {
	return parser.CognitiveComplexity(body, language)
}

// nestingDepth wraps parser.NestingDepth for use in metrics computation.
func nestingDepth(body, language string) int {
	return parser.NestingDepth(body, language)
}
