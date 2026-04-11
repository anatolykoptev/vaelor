package research

import (
	"math"
	"sort"
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

	// Sort descending by score.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].seedScore > candidates[j].seedScore
	})

	// Greedy selection within token budget.
	remaining := maxTokens
	for _, c := range candidates {
		cost := estimateTokens(c, includeBody)
		if remaining-cost < 0 && len(kept) > 0 {
			pruned++
			continue
		}
		remaining -= cost
		kept = append(kept, c)
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

// filterSymbolsByQuery returns only symbols whose names contain at least one query term.
// Falls back to all symbols if no terms match (avoids empty output).
func filterSymbolsByQuery(symbols []*parser.Symbol, queryTerms []string) []*parser.Symbol {
	if len(queryTerms) == 0 {
		return symbols
	}

	var matched []*parser.Symbol
	for _, sym := range symbols {
		lower := strings.ToLower(sym.Name)
		for _, term := range queryTerms {
			if strings.Contains(lower, strings.ToLower(term)) {
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
