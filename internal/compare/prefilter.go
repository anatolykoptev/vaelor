package compare

import (
	"unicode"
)

// overlapSkipThreshold is the minimum token overlap coefficient below which
// two symbol bodies are considered clearly different and fuzzy name matching
// is skipped.
const overlapSkipThreshold = 0.15

// extractIdentifiers splits body into identifier tokens (letters/digits/underscores,
// length >= 3) and returns a multiset mapping each token to its count.
func extractIdentifiers(body string) map[string]int {
	result := make(map[string]int)
	runes := []rune(body)
	n := len(runes)
	i := 0
	for i < n {
		if isIdentRune(runes[i]) {
			j := i + 1
			for j < n && isIdentRune(runes[j]) {
				j++
			}
			token := string(runes[i:j])
			if len([]rune(token)) >= 3 {
				result[token]++
			}
			i = j
		} else {
			i++
		}
	}
	return result
}

// isIdentRune reports whether r is a valid identifier character.
func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// multisetSize returns the sum of all counts in m.
func multisetSize(m map[string]int) int {
	total := 0
	for _, count := range m {
		total += count
	}
	return total
}

// tokenOverlap computes the overlap coefficient between the identifier multisets
// of bodyA and bodyB: |A ∩ B| / min(|A|, |B|).
// Returns 0.0 if either body has no identifiers.
func tokenOverlap(bodyA, bodyB string) float64 {
	a := extractIdentifiers(bodyA)
	b := extractIdentifiers(bodyB)

	sizeA := multisetSize(a)
	sizeB := multisetSize(b)

	if sizeA == 0 || sizeB == 0 {
		return 0.0
	}

	// Compute intersection size using min counts.
	intersection := 0
	for token, countA := range a {
		if countB, ok := b[token]; ok {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}

	minSize := sizeA
	if sizeB < minSize {
		minSize = sizeB
	}

	return float64(intersection) / float64(minSize)
}
