package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/clean"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// cyclomaticComplexity estimates McCabe's cyclomatic complexity from a function body string.
// This is a heuristic based on keyword counting (not full AST), but is fast and
// language-agnostic. Base complexity is 1. Comments are stripped before analysis.
//
// Counted keywords: if, else if, elif, for, while, case, catch, except, &&, ||
func cyclomaticComplexity(body, language string) int {
	if body == "" {
		return 1
	}

	body = clean.StripComments(body, language)

	cc := 1

	// Decision-point keywords. Order matters: "else if" and "elif" must be
	// checked before bare "if" to avoid double-counting.
	type pattern struct {
		text      string
		increment int
	}
	keywords := []pattern{
		{"else if ", 1},
		{"elif ", 1},
	}
	cleaned := body
	for _, kw := range keywords {
		count := strings.Count(cleaned, kw.text)
		cc += count * kw.increment
		cleaned = strings.ReplaceAll(cleaned, kw.text, strings.Repeat(" ", len(kw.text)))
	}

	singles := []string{
		"if ", "for ", "while ", "case ", "catch ", "catch(", "except ", "rescue ",
	}
	for _, kw := range singles {
		cc += strings.Count(cleaned, kw)
	}

	cc += strings.Count(cleaned, "&&")
	cc += strings.Count(cleaned, "||")

	return cc
}

// cognitiveComplexity wraps parser.CognitiveComplexity for use in metrics computation.
func cognitiveComplexity(body, language string) int {
	return parser.CognitiveComplexity(body, language)
}

// nestingDepth wraps parser.NestingDepth for use in metrics computation.
func nestingDepth(body, language string) int {
	return parser.NestingDepth(body, language)
}
