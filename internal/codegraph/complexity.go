package codegraph

import "strings"

// symbolComplexity estimates cyclomatic complexity from a function body.
// Heuristic keyword counting (not full AST), fast and language-agnostic.
func symbolComplexity(body string) int {
	if body == "" {
		return 1
	}
	cc := 1
	type kw struct {
		text string
		inc  int
	}
	multiWord := []kw{{"else if ", 1}, {"elif ", 1}, {"} else if ", 1}}
	cleaned := body
	for _, k := range multiWord {
		count := strings.Count(cleaned, k.text)
		cc += count * k.inc
		cleaned = strings.ReplaceAll(cleaned, k.text, strings.Repeat(" ", len(k.text)))
	}
	singles := []string{"if ", "for ", "while ", "case ", "catch ", "catch(", "except ", "rescue "}
	for _, k := range singles {
		cc += strings.Count(cleaned, k)
	}
	cc += strings.Count(cleaned, "&&")
	cc += strings.Count(cleaned, "||")
	return cc
}
