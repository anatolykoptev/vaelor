package embeddings

import (
	"sort"

	"github.com/anatolykoptev/go-kit/rerank"
)

// rrfK is the standard RRF constant that smooths rank differences.
const rrfK = 60

// KeywordHit represents a grep/code_search match mapped to a symbol.
type KeywordHit struct {
	FilePath   string
	SymbolName string
	Line       int
}

// SparseHit is a single SPLADE sparse-retrieval result. Shape is symmetric with
// KeywordHit so P4 can fuse all three arms via the same FilePath:SymbolName key
// already used by MergeRRF. Source is always "sparse" before fusion.
type SparseHit struct {
	FilePath   string
	SymbolName string
	Line       int // start_line from the index row
}

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source   string  // "semantic", "keyword", or "hybrid"
	RRFScore float64 // combined reciprocal rank fusion score
}

// RRFWeights are per-retriever weights for the WeightedRRF fusion in MergeRRF.
// Both fields must be ≥ 0 (negative values panic in go-kit/rerank.WeightedRRF
// — programmer error, not runtime). Weights == (1.0, 1.0) is mathematically
// identical to plain RRF (Cormack-Clarke 2009); use that for byte-identical
// rollback.
type RRFWeights struct {
	Semantic float64
	Keyword  float64
}

// DefaultRRFWeights returns the (1.0, 1.0) baseline that reproduces the
// pre-Stream-1 unweighted rerank.RRF behavior. Tests and callers that don't
// thread per-deployment config should use this.
func DefaultRRFWeights() RRFWeights {
	return RRFWeights{Semantic: 1.0, Keyword: 1.0}
}

// MergeRRF combines semantic and keyword results using Reciprocal Rank Fusion.
// Backed by go-kit/rerank.WeightedRRF (Cormack-Clarke 2009 with per-list
// weights, k=60). Key = FilePath + ":" + SymbolName for dedup; results in both
// lists get boosted ("hybrid" source). Returns at most topK results.
//
// weights are env-driven (RRF_WEIGHT_SEMANTIC / RRF_WEIGHT_KEYWORD) and
// surfaced via Prometheus gauge gocode_rrf_weights{retriever}. Pass
// DefaultRRFWeights() for byte-identical legacy behavior.
func MergeRRF(semantic []SearchResult, keyword []KeywordHit, topK int, weights RRFWeights) []HybridResult {
	if len(semantic) == 0 && len(keyword) == 0 {
		return nil
	}

	type entry struct {
		result     SearchResult
		inSemantic bool
		inKeyword  bool
	}
	index := make(map[string]*entry, len(semantic)+len(keyword))
	semIDs := make([]string, 0, len(semantic))
	kwIDs := make([]string, 0, len(keyword))

	for _, r := range semantic {
		key := r.FilePath + ":" + r.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &entry{result: r}
		}
		index[key].inSemantic = true
		semIDs = append(semIDs, key)
	}

	for _, h := range keyword {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &entry{result: SearchResult{
				FilePath:   h.FilePath,
				SymbolName: h.SymbolName,
				StartLine:  h.Line,
			}}
		}
		index[key].inKeyword = true
		kwIDs = append(kwIDs, key)
	}

	fused := rerank.WeightedRRF(rrfK, []float64{weights.Semantic, weights.Keyword}, semIDs, kwIDs)

	results := make([]HybridResult, 0, len(fused))
	for _, f := range fused {
		e := index[f.ID]
		if e == nil {
			continue
		}
		var source string
		switch {
		case e.inSemantic && e.inKeyword:
			source = "hybrid"
		case e.inKeyword:
			source = "keyword"
		default:
			source = "semantic"
		}
		results = append(results, HybridResult{
			SearchResult: e.result,
			Source:       source,
			RRFScore:     f.Score,
		})
	}

	// rerank.WeightedRRF returns sorted by score desc with stable tie-break,
	// but the existing test contract checks topK truncation independently;
	// preserve explicit sort for byte-identical fallback if upstream changes.
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}
