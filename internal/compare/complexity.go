package compare

import (
	"strings"
)

// cyclomaticComplexity estimates McCabe's cyclomatic complexity from a function body string.
// This is a heuristic based on keyword counting (not full AST), but is fast and
// language-agnostic. Base complexity is 1.
//
// Counted keywords: if, else if, elif, for, while, case, catch, except, &&, ||
func cyclomaticComplexity(body string) int {
	if body == "" {
		return 1
	}

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
		{"} else if ", 1},
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
