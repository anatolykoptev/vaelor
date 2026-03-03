package parser

import "strings"

// Complexity estimates cyclomatic complexity from a function body.
// Heuristic keyword counting (not full AST), fast and language-agnostic.
// Returns 0 for empty body, 1+ for non-empty (1 = linear, no branches).
func Complexity(body string) int {
	if body == "" {
		return 0
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

// isPythonBody detects Python code by checking for "def " without braces.
func isPythonBody(body string) bool {
	return strings.Contains(body, "def ") && !strings.Contains(body, "{")
}

// decision keywords that increment cognitive complexity.
var decisionKeywords = []string{"if ", "for ", "while ", "switch ", "catch ", "except "}

// continuation keywords — flat +1, no nesting bonus (SonarQube rule).
var continuationKeywords = []string{"else if ", "elif "}

// CognitiveComplexity estimates cognitive complexity from a function body.
// Uses SonarQube-style rules: decision keywords score +1 + nesting depth,
// continuations (else if, elif) score flat +1, logical operators score +1 each.
// For Python (no braces): uses indent-based nesting.
func CognitiveComplexity(body string) int {
	if body == "" {
		return 0
	}

	if isPythonBody(body) {
		return cognitiveComplexityPython(body)
	}
	return cognitiveComplexityBrace(body)
}

// cognitiveComplexityBrace computes cognitive complexity for brace-based languages.
// The function's own opening brace is excluded from nesting count.
func cognitiveComplexityBrace(body string) int {
	score := 0
	nesting := 0
	firstBrace := false
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Continuations: flat +1, no nesting bonus (SonarQube rule).
		if matchesContinuation(trimmed) {
			score++
		} else if matchesDecision(trimmed) {
			// Decision keywords: +1 + nesting depth.
			score += 1 + nesting
		}

		// Logical operators: +1 each.
		score += strings.Count(trimmed, "&&")
		score += strings.Count(trimmed, "||")

		// Update nesting from braces, skipping function's own opening brace.
		opens := strings.Count(trimmed, "{")
		closes := strings.Count(trimmed, "}")
		if !firstBrace && opens > 0 {
			firstBrace = true
			opens--
		}
		nesting += opens - closes
		if nesting < 0 {
			nesting = 0
		}
	}

	return score
}

// NestingDepth returns the maximum brace nesting depth in a function body.
// For Python (no braces): uses indent-based depth.
func NestingDepth(body string) int {
	if body == "" {
		return 0
	}
	if isPythonBody(body) {
		return nestingDepthPython(body)
	}
	return nestingDepthBrace(body)
}

// nestingDepthBrace tracks max brace depth.
func nestingDepthBrace(body string) int {
	depth, maxDepth := 0, 0
	for _, ch := range body {
		switch ch {
		case '{':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		case '}':
			depth--
			if depth < 0 {
				depth = 0
			}
		}
	}
	return maxDepth
}

