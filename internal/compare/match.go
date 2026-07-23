package compare

import (
	"context"

	"github.com/anatolykoptev/vaelor/internal/parser"
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
//
// ctx is checked at pass boundaries and inside the O(n×m) fuzzy loop so a
// canceled ctx bails promptly with whatever matches were computed so far
// (#580). The remaining unmatched symbols become gaps.
func MatchSymbols(ctx context.Context, symbolsA, symbolsB []*parser.Symbol, classifier LLMClassifier) []SymbolMatch {
	var matches []SymbolMatch

	unmatchedA := make([]*parser.Symbol, len(symbolsA))
	copy(unmatchedA, symbolsA)
	unmatchedB := make([]*parser.Symbol, len(symbolsB))
	copy(unmatchedB, symbolsB)

	// Pass 1: exact match by name + kind.
	unmatchedA, unmatchedB, exactMatches := matchExact(ctx, unmatchedA, unmatchedB)
	matches = append(matches, exactMatches...)

	if ctx.Err() != nil {
		return appendGapMatches(matches, unmatchedA, unmatchedB)
	}

	// Pass 2: fuzzy match by name similarity, same kind.
	unmatchedA, unmatchedB, fuzzyMatches := matchFuzzy(ctx, unmatchedA, unmatchedB)
	matches = append(matches, fuzzyMatches...)

	if ctx.Err() != nil {
		return appendGapMatches(matches, unmatchedA, unmatchedB)
	}

	// Pass 3: signature match (catches renames where code is identical).
	unmatchedA, unmatchedB, sigMatches := matchSignature(ctx, unmatchedA, unmatchedB)
	matches = append(matches, sigMatches...)

	if ctx.Err() != nil {
		return appendGapMatches(matches, unmatchedA, unmatchedB)
	}

	// Pass 4: semantic match via LLM classifier.
	if classifier != nil && (len(unmatchedA) > 0 || len(unmatchedB) > 0) {
		if semanticMatches, err := classifier.ClassifySymbols(unmatchedA, unmatchedB); err == nil {
			matches = append(matches, semanticMatches...)
			// Remove semantically matched symbols from the unmatched slices.
			unmatchedA, unmatchedB = removeSemanticMatched(unmatchedA, unmatchedB, semanticMatches)
		}
	}

	return appendGapMatches(matches, unmatchedA, unmatchedB)
}

// appendGapMatches adds gap entries for remaining unmatched symbols and
// returns the combined slice. Extracted so each ctx.Err() bail point in
// MatchSymbols can reuse it without duplicating the gap loop.
func appendGapMatches(matches []SymbolMatch, unmatchedA, unmatchedB []*parser.Symbol) []SymbolMatch {
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
// Checks ctx every matchExactCtxBatch symbols so a canceled ctx bails promptly
// instead of completing an O(n×m) scan past the soft deadline (#566).
func matchExact(ctx context.Context, a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for i, symA := range a {
		if i%matchExactCtxBatch == 0 && ctx.Err() != nil {
			// Bail: remaining symbols in a go to unmatchedA, unused b to unmatchedB.
			unmatchedA = append(unmatchedA, a[i:]...)
			break
		}
		idx := findExact(symA, b, usedB)
		if idx >= 0 {
			usedB[idx] = true
			mt := MatchExact
			if symA.BodyHash != 0 && b[idx].BodyHash != 0 {
				if symA.BodyHash != b[idx].BodyHash {
					mt = MatchModified
				} else if symA.File != b[idx].File {
					mt = MatchMoved
				}
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

// matchExactCtxBatch is the interval (in symbols) between ctx.Err() checks in
// matchExact. findExact is O(m) per symbol, so checking every 512 symbols keeps
// the worst-case unbounded window at ~512×m iterations — single-digit seconds
// even for 10k-symbol repos, well inside the soft deadline's remaining budget.
const matchExactCtxBatch = 512

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
// and the remaining unmatched slices. Checks ctx at loop granularity so a
// canceled ctx bails mid-scan (#580).
func matchFuzzy(ctx context.Context, a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		if ctx.Err() != nil {
			// Bail: remaining a-symbols become unmatched.
			unmatchedA = append(unmatchedA, symA)
			continue
		}
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
// Checks ctx at loop granularity (#580).
func matchSignature(ctx context.Context, a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		if ctx.Err() != nil {
			unmatchedA = append(unmatchedA, symA)
			continue
		}
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
