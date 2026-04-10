package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// fuzzyThreshold is the minimum name similarity score to accept a fuzzy match.
const fuzzyThreshold = 0.7

// LLMClassifier is a consumer-defined interface for semantic symbol pairing.
// It is called as a last resort after exact and fuzzy matching are exhausted.
type LLMClassifier interface {
	ClassifySymbols(a, b []*parser.Symbol) ([]SymbolMatch, error)
}

// MatchSymbols aligns symbols from two repositories using four passes:
//  1. Exact: match by identical name and kind (score 1.0).
//  2. Fuzzy: match by name similarity >= fuzzyThreshold, same kind (score = similarity).
//  3. Signature: match by identical/similar signature + body hash (catches renames).
//  4. Semantic: delegate remaining unmatched pairs to classifier (if non-nil).
//
// Symbols with no counterpart become gap entries (SymbolA or SymbolB is nil).
func MatchSymbols(symbolsA, symbolsB []*parser.Symbol, classifier LLMClassifier) []SymbolMatch {
	var matches []SymbolMatch

	unmatchedA := make([]*parser.Symbol, len(symbolsA))
	copy(unmatchedA, symbolsA)
	unmatchedB := make([]*parser.Symbol, len(symbolsB))
	copy(unmatchedB, symbolsB)

	// Pass 1: exact match by name + kind.
	unmatchedA, unmatchedB, exactMatches := matchExact(unmatchedA, unmatchedB)
	matches = append(matches, exactMatches...)

	// Pass 2: fuzzy match by name similarity, same kind.
	unmatchedA, unmatchedB, fuzzyMatches := matchFuzzy(unmatchedA, unmatchedB)
	matches = append(matches, fuzzyMatches...)

	// Pass 3: signature match (catches renames where code is identical).
	unmatchedA, unmatchedB, sigMatches := matchSignature(unmatchedA, unmatchedB)
	matches = append(matches, sigMatches...)

	// Pass 4: semantic match via LLM classifier.
	if classifier != nil && (len(unmatchedA) > 0 || len(unmatchedB) > 0) {
		if semanticMatches, err := classifier.ClassifySymbols(unmatchedA, unmatchedB); err == nil {
			matches = append(matches, semanticMatches...)
			// Remove semantically matched symbols from the unmatched slices.
			unmatchedA, unmatchedB = removeSemanticMatched(unmatchedA, unmatchedB, semanticMatches)
		}
	}

	// Remaining unmatched symbols become gaps.
	for _, sym := range unmatchedA {
		matches = append(matches, SymbolMatch{
			SymbolA:   sym,
			MatchType: MatchGap,
			Category:  string(sym.Kind),
		})
	}
	for _, sym := range unmatchedB {
		matches = append(matches, SymbolMatch{
			SymbolB:   sym,
			MatchType: MatchGap,
			Category:  string(sym.Kind),
		})
	}

	return matches
}

// matchExact returns exact matches (name + kind) and the remaining unmatched slices.
func matchExact(a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		idx := findExact(symA, b, usedB)
		if idx >= 0 {
			usedB[idx] = true
			mt := MatchExact
			if symA.BodyHash != 0 && b[idx].BodyHash != 0 && symA.BodyHash != b[idx].BodyHash {
				mt = MatchModified
			}
			matches = append(matches, SymbolMatch{
				SymbolA:   symA,
				SymbolB:   b[idx],
				MatchType: mt,
				Category:  string(symA.Kind),
				Score:     1.0,
			})
		} else {
			unmatchedA = append(unmatchedA, symA)
		}
	}

	for i, sym := range b {
		if !usedB[i] {
			unmatchedB = append(unmatchedB, sym)
		}
	}

	return unmatchedA, unmatchedB, matches
}

// findExact returns the index of the first symbol in candidates that has the
// same Name and Kind as target, or -1 if none is found.
func findExact(target *parser.Symbol, candidates []*parser.Symbol, used []bool) int {
	for i, c := range candidates {
		if !used[i] && c.Name == target.Name && c.Kind == target.Kind {
			return i
		}
	}
	return -1
}

// matchFuzzy returns fuzzy matches (same kind, name similarity >= fuzzyThreshold)
// and the remaining unmatched slices.
func matchFuzzy(a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		idx, score := findFuzzy(symA, b, usedB)
		if idx >= 0 {
			usedB[idx] = true
			matches = append(matches, SymbolMatch{
				SymbolA:   symA,
				SymbolB:   b[idx],
				MatchType: MatchFuzzy,
				Category:  string(symA.Kind),
				Score:     score,
			})
		} else {
			unmatchedA = append(unmatchedA, symA)
		}
	}

	for i, sym := range b {
		if !usedB[i] {
			unmatchedB = append(unmatchedB, sym)
		}
	}

	return unmatchedA, unmatchedB, matches
}

// findFuzzy returns the index and similarity score of the best fuzzy candidate
// for target, requiring same Kind and score >= fuzzyThreshold. Returns -1 if none.
func findFuzzy(target *parser.Symbol, candidates []*parser.Symbol, used []bool) (int, float64) {
	bestIdx := -1
	bestScore := 0.0

	for i, c := range candidates {
		if used[i] || c.Kind != target.Kind {
			continue
		}
		// Pre-filter: skip if token overlap is too low.
		if target.Body != "" && c.Body != "" {
			if tokenOverlap(target.Body, c.Body) < overlapSkipThreshold {
				continue
			}
		}
		score := nameSimilarity(target.Name, c.Name)
		if score >= fuzzyThreshold && score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx, bestScore
}

// signatureMatchThreshold is the minimum similarity for signature-based matching.
const signatureMatchThreshold = 0.85

// matchSignature matches symbols with identical or very similar signatures
// and the same body hash. Catches renames where name changed but code didn't.
func matchSignature(a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		if symA.Signature == "" {
			unmatchedA = append(unmatchedA, symA)
			continue
		}

		idx, score := findSignatureMatch(symA, b, usedB)
		if idx >= 0 {
			usedB[idx] = true
			matches = append(matches, SymbolMatch{
				SymbolA:   symA,
				SymbolB:   b[idx],
				MatchType: MatchRenamed,
				Category:  string(symA.Kind),
				Score:     score,
			})
		} else {
			unmatchedA = append(unmatchedA, symA)
		}
	}

	for i, sym := range b {
		if !usedB[i] {
			unmatchedB = append(unmatchedB, sym)
		}
	}

	return unmatchedA, unmatchedB, matches
}

// findSignatureMatch finds the best candidate with same kind, matching signature,
// and matching body hash (if available). Returns -1 if no match found.
func findSignatureMatch(target *parser.Symbol, candidates []*parser.Symbol, used []bool) (int, float64) {
	bestIdx := -1
	bestScore := 0.0

	for i, c := range candidates {
		if used[i] || c.Kind != target.Kind || c.Signature == "" {
			continue
		}

		sigScore := nameSimilarity(target.Signature, c.Signature)
		if sigScore < signatureMatchThreshold {
			continue
		}

		// Boost score if body hashes also match.
		score := sigScore
		if target.BodyHash != 0 && c.BodyHash != 0 && target.BodyHash == c.BodyHash {
			score = (sigScore + 1.0) / 2
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx, bestScore
}

// removeSemanticMatched removes symbols from unmatchedA and unmatchedB that
// appear in the semantic matches returned by the classifier.
func removeSemanticMatched(unmatchedA, unmatchedB []*parser.Symbol, semanticMatches []SymbolMatch) ([]*parser.Symbol, []*parser.Symbol) {
	matchedA := make(map[*parser.Symbol]bool, len(semanticMatches))
	matchedB := make(map[*parser.Symbol]bool, len(semanticMatches))
	for _, m := range semanticMatches {
		if m.SymbolA != nil {
			matchedA[m.SymbolA] = true
		}
		if m.SymbolB != nil {
			matchedB[m.SymbolB] = true
		}
	}

	filteredA := unmatchedA[:0:0]
	for _, s := range unmatchedA {
		if !matchedA[s] {
			filteredA = append(filteredA, s)
		}
	}

	filteredB := unmatchedB[:0:0]
	for _, s := range unmatchedB {
		if !matchedB[s] {
			filteredB = append(filteredB, s)
		}
	}

	return filteredA, filteredB
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
