package embeddings

import "sort"

// rrfK is the standard RRF constant that smooths rank differences.
const rrfK = 60

// KeywordHit represents a grep/code_search match mapped to a symbol.
type KeywordHit struct {
	FilePath   string
	SymbolName string
	Line       int
}

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source   string  // "semantic", "keyword", or "hybrid"
	RRFScore float64 // combined reciprocal rank fusion score
}

// MergeRRF combines semantic and keyword results using Reciprocal Rank Fusion.
// Results appearing in both lists get boosted. Returns at most topK results.
// Key = FilePath + ":" + SymbolName for deduplication.
// RRF score = sum of 1/(k+rank+1) where k=60 (standard constant).
func MergeRRF(semantic []SearchResult, keyword []KeywordHit, topK int) []HybridResult {
	type entry struct {
		result       SearchResult
		semanticRank int // 0 = not present
		keywordRank  int // 0 = not present
	}

	index := make(map[string]*entry)
	order := make([]string, 0, len(semantic)+len(keyword))

	// Index semantic results (1-based rank).
	for i, r := range semantic {
		key := r.FilePath + ":" + r.SymbolName
		if _, ok := index[key]; !ok {
			order = append(order, key)
			index[key] = &entry{result: r}
		}
		index[key].semanticRank = i + 1
	}

	// Index keyword results (1-based rank).
	for i, h := range keyword {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			order = append(order, key)
			index[key] = &entry{result: SearchResult{
				FilePath:   h.FilePath,
				SymbolName: h.SymbolName,
				StartLine:  h.Line,
			}}
		}
		index[key].keywordRank = i + 1
	}

	// Compute RRF score and build output.
	results := make([]HybridResult, 0, len(order))
	for _, key := range order {
		e := index[key]
		var score float64
		if e.semanticRank > 0 {
			score += 1.0 / float64(rrfK+e.semanticRank)
		}
		if e.keywordRank > 0 {
			score += 1.0 / float64(rrfK+e.keywordRank)
		}

		var source string
		switch {
		case e.semanticRank > 0 && e.keywordRank > 0:
			source = "hybrid"
		case e.keywordRank > 0:
			source = "keyword"
		default:
			source = "semantic"
		}

		results = append(results, HybridResult{
			SearchResult: e.result,
			Source:       source,
			RRFScore:     score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}
