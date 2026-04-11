package research

import (
	"math"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	// hopDecay is the score multiplier per hop distance from seeds.
	// distance=0 → 1.0, distance=1 → 0.6, distance=2 → 0.36, etc.
	hopDecay = 0.6

	// charsPerToken is a rough approximation: ~4 chars per token (GPT/Gemini average).
	charsPerToken = 4

	// overhead per file entry in the map (path line + padding).
	mapOverheadCharsPerFile = 80

	// mmrLambda trades relevance vs diversity in Maximal Marginal Relevance.
	// score = λ × relevance − (1−λ) × max_similarity_to_already_picked
	// λ=0.7 leans toward relevance but actively penalises near-duplicates.
	mmrLambda = 0.7
)

// scoredFile pairs an expandResult with its combined relevance score.
type scoredFile struct {
	expand    expandResult
	seedScore float64 // BM25F/RRF score of nearest seed in this file (0 if not a seed)
	symbols   []*parser.Symbol
}

// pruneToTokenBudget applies distance-decay scoring and selects files that fit
// within the token budget, returning them ordered by score descending.
//
// seedScores maps relPath → seed score (0 for non-seed expanded files).
// fileSymbols maps relPath → symbols filtered to be relevant to the query.
// maxTokens is the target token budget.
func pruneToTokenBudget(
	expanded []expandResult,
	seedScores map[string]float64,
	fileSymbols map[string][]*parser.Symbol,
	maxTokens int,
	includeBody bool,
) (kept []scoredFile, pruned int) {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	// Score each file.
	candidates := make([]scoredFile, 0, len(expanded))
	for _, ex := range expanded {
		base := seedScores[ex.relPath]
		if base == 0 {
			base = 0.1 // small floor for expanded-only files
		}
		decay := math.Pow(hopDecay, float64(ex.distance))
		score := base * decay

		candidates = append(candidates, scoredFile{
			expand:    ex,
			seedScore: score,
			symbols:   fileSymbols[ex.relPath],
		})
	}

	// Pre-compute lower-cased identifier sets per candidate for Jaccard similarity.
	idSets := make([]map[string]bool, len(candidates))
	for i, c := range candidates {
		set := make(map[string]bool, len(c.symbols))
		for _, s := range c.symbols {
			set[strings.ToLower(s.Name)] = true
		}
		idSets[i] = set
	}

	// MMR selection: iteratively pick the candidate that maximises
	// λ × relevance − (1−λ) × max_similarity_to_already_picked.
	picked := make([]int, 0, len(candidates))
	pickedMask := make([]bool, len(candidates))
	remaining := maxTokens

	for len(picked) < len(candidates) {
		bestIdx := -1
		bestScore := math.Inf(-1)
		for i, c := range candidates {
			if pickedMask[i] {
				continue
			}
			redundancy := 0.0
			for _, j := range picked {
				sim := jaccard(idSets[i], idSets[j])
				if sim > redundancy {
					redundancy = sim
				}
			}
			mmrScore := mmrLambda*c.seedScore - (1-mmrLambda)*redundancy
			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		c := candidates[bestIdx]
		cost := estimateTokens(c, includeBody)
		if remaining-cost < 0 && len(kept) > 0 {
			pruned++
			pickedMask[bestIdx] = true
			continue
		}
		remaining -= cost
		kept = append(kept, c)
		picked = append(picked, bestIdx)
		pickedMask[bestIdx] = true
	}
	return kept, pruned
}

// estimateTokens approximates the token cost of including a file in the map.
// When includeBody is false, body content is excluded from the estimate to match
// what RenderMap actually emits (bodies are only rendered when IncludeBody=true).
func estimateTokens(sf scoredFile, includeBody bool) int {
	chars := mapOverheadCharsPerFile
	for _, sym := range sf.symbols {
		chars += len(sym.Name) + 30 // name + signature overhead
		if includeBody && sym.Body != "" {
			chars += len(sym.Body)
		}
	}
	return chars / charsPerToken
}

// jaccard returns the Jaccard similarity coefficient between two symbol-name sets.
// Returns 0 if either set is nil or empty.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// filterSymbolsByQuery returns only symbols whose names contain at least one query term.
// Falls back to all symbols if no terms match (avoids empty output).
func filterSymbolsByQuery(symbols []*parser.Symbol, queryTerms []string) []*parser.Symbol {
	if len(queryTerms) == 0 {
		return symbols
	}

	var matched []*parser.Symbol
	for _, sym := range symbols {
		nameLower := strings.ToLower(sym.Name)
		docLower := strings.ToLower(sym.DocComment)
		for _, term := range queryTerms {
			t := strings.ToLower(term)
			if strings.Contains(nameLower, t) || (docLower != "" && strings.Contains(docLower, t)) {
				matched = append(matched, sym)
				break
			}
		}
	}
	if len(matched) == 0 {
		// No match — return functions/methods only (skip constants/vars) as fallback.
		for _, sym := range symbols {
			if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod ||
				sym.Kind == parser.KindClass || sym.Kind == parser.KindInterface ||
				sym.Kind == parser.KindStruct {
				matched = append(matched, sym)
			}
		}
	}
	return matched
}
