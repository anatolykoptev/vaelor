package parser

import "strings"

const pythonIndentSize = 4 // standard Python indentation width

// cognitiveComplexityPython computes cognitive complexity using indent-based nesting.
// The def line is skipped; base indent is set from the first body line.
func cognitiveComplexityPython(body string) int {
	score := 0
	baseIndent := -1
	pastDef := false
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !pastDef {
			if strings.HasPrefix(trimmed, "def ") {
				pastDef = true
				continue
			}
		}

		indent := countLeadingSpaces(line)
		if baseIndent < 0 {
			baseIndent = indent
		}
		nesting := (indent - baseIndent) / pythonIndentSize
		if nesting < 0 {
			nesting = 0
		}

		if matchesContinuation(trimmed) {
			score++
		} else if matchesDecision(trimmed) {
			score += 1 + nesting
		}

		cleaned := stripStringLiterals(trimmed)
		score += strings.Count(cleaned, " and ")
		score += strings.Count(cleaned, " or ")
	}

	return score
}

// nestingDepthPython tracks max indent-based depth, skipping the def line.
func nestingDepthPython(body string) int {
	baseIndent := -1
	pastDef := false
	maxDepth := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !pastDef {
			if strings.HasPrefix(trimmed, "def ") {
				pastDef = true
				continue
			}
		}
		indent := countLeadingSpaces(line)
		if baseIndent < 0 {
			baseIndent = indent
		}
		depth := (indent - baseIndent) / pythonIndentSize
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}

func matchesDecision(line string) bool {
	for _, kw := range decisionKeywords {
		if strings.HasPrefix(line, kw) || strings.Contains(line, " "+kw) {
			return true
		}
	}
	return false
}

func matchesContinuation(line string) bool {
	for _, kw := range continuationKeywords {
		if strings.HasPrefix(line, kw) || strings.Contains(line, "} "+kw) {
			return true
		}
	}
	return false
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		switch ch {
		case ' ':
			n++
		case '\t':
			n += pythonIndentSize
		default:
			return n
		}
	}
	return n
}
