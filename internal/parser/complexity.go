package parser

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/clean"
)

// stripStringLiterals blanks out content inside string literals so that
// keywords/operators inside strings are not counted by heuristic analysis.
// Handles double-quoted strings (with \" escapes) and Go raw strings (backtick).
func stripStringLiterals(s string) string {
	buf := []byte(s)
	i := 0
	for i < len(buf) {
		switch buf[i] {
		case '"':
			// Double-quoted string: blank content, keep quotes.
			i++
			for i < len(buf) && buf[i] != '"' {
				if buf[i] == '\\' && i+1 < len(buf) {
					buf[i] = ' '
					i++
				}
				buf[i] = ' '
				i++
			}
			if i < len(buf) {
				i++ // skip closing quote
			}
		case '`':
			// Raw string (Go backtick): blank content, keep delimiters.
			i++
			for i < len(buf) && buf[i] != '`' {
				buf[i] = ' '
				i++
			}
			if i < len(buf) {
				i++ // skip closing backtick
			}
		default:
			i++
		}
	}
	return string(buf)
}

// Complexity estimates cyclomatic complexity from a function body.
// Heuristic keyword counting (not full AST), fast and language-agnostic.
// Comments are stripped before counting. Returns 1 for empty or comment-only
// bodies (base complexity of any function is 1).
func Complexity(body, language string) int {
	body = clean.StripComments(body, language)
	if strings.TrimSpace(body) == "" {
		return 1
	}
	cc := 1
	type kw struct {
		text string
		inc  int
	}
	multiWord := []kw{{"else if ", 1}, {"elif ", 1}}
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

// decision keywords that increment cognitive complexity.
var decisionKeywords = []string{"if ", "for ", "while ", "switch ", "catch ", "except "}

// continuation keywords — flat +1, no nesting bonus (SonarQube rule).
var continuationKeywords = []string{"else if ", "elif "}

// CognitiveComplexity estimates cognitive complexity from a function body.
// Uses SonarQube-style rules: decision keywords score +1 + nesting depth,
// continuations (else if, elif) score flat +1, logical operators score +1 each.
// For Python: uses indent-based nesting. Language is used to select the strategy.
func CognitiveComplexity(body, language string) int {
	if body == "" {
		return 0
	}

	if language == "python" {
		return cognitiveComplexityPython(body)
	}
	return cognitiveComplexityBrace(body, language)
}

// cognitiveComplexityBrace computes cognitive complexity for brace-based languages.
// The function's own opening brace is excluded from nesting count.
// Comments and string literals are stripped before analysis.
func cognitiveComplexityBrace(body, language string) int {
	body = clean.StripComments(body, language)

	score := 0
	nesting := 0
	firstBrace := false
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		cleaned := stripStringLiterals(trimmed)

		// Continuations: flat +1, no nesting bonus (SonarQube rule).
		if matchesContinuation(cleaned) {
			score++
		} else if matchesDecision(cleaned) {
			// Decision keywords: +1 + nesting depth.
			score += 1 + nesting
		}

		// Logical operators: +1 each (on string-stripped version).
		score += strings.Count(cleaned, "&&")
		score += strings.Count(cleaned, "||")

		// Update nesting from braces (on string-stripped version).
		opens := strings.Count(cleaned, "{")
		closes := strings.Count(cleaned, "}")
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
// The function's own opening brace is excluded (consistent with CognitiveComplexity).
// For Python: uses indent-based depth.
func NestingDepth(body, language string) int {
	if body == "" {
		return 0
	}
	if language == "python" {
		return nestingDepthPython(body)
	}
	return nestingDepthBrace(body, language)
}

// nestingDepthBrace tracks max brace depth, skipping the function's own opening brace.
// Comments and string literals are stripped before counting.
func nestingDepthBrace(body, language string) int {
	body = clean.StripComments(body, language)
	stripped := stripStringLiterals(body)

	depth, maxDepth := 0, 0
	firstBrace := false
	for _, ch := range stripped {
		switch ch {
		case '{':
			if !firstBrace {
				firstBrace = true
				continue
			}
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
