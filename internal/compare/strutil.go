package compare

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isExported reports whether a symbol name is exported (starts with an uppercase letter).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// levenshtein computes the edit distance between strings a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use two rows to save memory.
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := range prev {
		prev[j] = j
	}

	for i := range la {
		curr[0] = i + 1
		for j := range lb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			curr[j+1] = min(
				curr[j]+1,
				min(prev[j+1]+1, prev[j]+cost),
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

// nameSimilarity returns a score in [0, 1] based on the normalised Levenshtein
// distance: 1.0 means identical, 0.0 means completely different.
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := max(len([]rune(a)), len([]rune(b)))
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

// findMatchingParen finds the closing paren matching the open paren at index open in s.
// Returns -1 if not found.
func findMatchingParen(s string, open int) int {
	depth := 1
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isIdent reports whether s consists only of identifier-valid characters.
func isIdent(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '*' && r != '[' && r != ']' && r != ' ' {
			return false
		}
	}
	return true
}

// splitParams splits a parameter list by commas, respecting parentheses depth.
// Commas inside nested parens (e.g., func(K, V)) are not treated as separators.
func splitParams(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// extractParamList finds the parameter list string, skipping Go receivers.
func extractParamList(sig string, firstParen int) string {
	end := findMatchingParen(sig, firstParen)
	if end < 0 {
		return ""
	}
	// Check if there's another paren group after (means first was receiver).
	rest := sig[end+1:]
	nextParen := strings.IndexByte(rest, '(')
	if nextParen >= 0 {
		between := strings.TrimSpace(rest[:nextParen])
		if len(between) > 0 && isIdent(between) {
			nextEnd := findMatchingParen(rest, nextParen)
			if nextEnd >= 0 {
				return rest[nextParen+1 : nextEnd]
			}
		}
	}
	return sig[firstParen+1 : end]
}
