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

// GraphHit is a single graph-candidate or signal result. The same shape is used
// by the graph, hotspot, and recency RRF arms because all three are keyed by
// FilePath:SymbolName and carry the same symbol metadata.
// Source is set per-arm before fusion.
type GraphHit struct {
	FilePath   string
	SymbolName string
	SymbolKind string
	Line       int // start_line from the index row; 0 when unknown
}

// HotspotHit is a ranked list entry for the hotspot RRF arm (churn x complexity).
// It is a type alias for GraphHit so the two arms remain distinct in signatures.
type HotspotHit = GraphHit

// RecencyHit is a ranked list entry for the recency RRF arm (last-modified time).
// It is a type alias for GraphHit so the two arms remain distinct in signatures.
type RecencyHit = GraphHit

// Source constants for HybridResult.Source attribution.
const (
	sourceSemantic = "semantic"
	sourceKeyword  = "keyword"
	sourceSparse   = "sparse"
	sourceGraph    = "graph"
	sourceHotspot  = "hotspot"
	sourceRecency  = "recency"
	sourceHybrid   = "hybrid"
)

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source   string  // "semantic", "keyword", "sparse", "graph", "hotspot", "recency", or "hybrid"
	RRFScore float64 // combined reciprocal rank fusion score
}

// RRFWeights are per-retriever weights for the WeightedRRF fusion in MergeRRF.
// All fields must be ≥ 0 (negative values panic in go-kit/rerank.WeightedRRF
// — programmer error, not runtime). Weights == (1.0, 1.0, 1.0, 1.0, 1.0, 1.0) is
// the math identity used by tests; deployed defaults are controlled by
// cmd/go-code/config.go and keep Sparse/Hotspot/Recency dark-launched when
// their env weights are unset.
// Weights == (1.0, 1.0) in the 2-arm sense is mathematically identical to
// plain RRF (Cormack-Clarke 2009).
type RRFWeights struct {
	Semantic float64
	Keyword  float64
	// Sparse is the per-list weight for the SPLADE sparse-retrieval arm.
	// Default env value: 0.0 (dark-launched — plumbed but inert until A/B
	// in Phase 6 clears the gate). Post-A/B recommended value: 0.2–0.4
	// (below dense per research note in 2026-06-01 SPLADE landscape report).
	// Tune via RRF_WEIGHT_SPARSE env. Must be ≥ 0.
	Sparse float64
	// Graph is the per-list weight for the graph-candidate arm (Phase 1 graph-first
	// retrieval plan). Default env value: 0.0 (dark-launched — byte-identical until
	// A/B clears the gate). See RRF_WEIGHT_GRAPH in cmd/go-code/config.go.
	// Post-A/B recommended band: 0.2–0.4 (below dense per plan ADR).
	// Tune via RRF_WEIGHT_GRAPH env. Must be ≥ 0.
	Graph float64
	// Hotspot is the per-list weight for the churn x complexity signal arm.
	// Default env value: 0.0 (dark-launched — plumbed but inert until A/B
	// validates the quality gain). Post-A/B recommended band: 0.1–0.2.
	// Tune via RRF_WEIGHT_HOTSPOT env. Must be ≥ 0.
	Hotspot float64
	// Recency is the per-list weight for the last-modified time signal arm.
	// Default env value: 0.0 (dark-launched — plumbed but inert until A/B
	// validates the quality gain). Post-A/B recommended band: 0.05–0.15.
	// Tune via RRF_WEIGHT_RECENCY env. Must be ≥ 0.
	Recency float64
	// RankWindow is the Elasticsearch rank_window_size cap (issue #663): each
	// arm's ranked input list is truncated to its top-RankWindow entries BEFORE
	// WeightedRRF, so an item ranked beyond the window in every arm it appears
	// in cannot contribute to (or re-order) the fused output. Sentinel: <= 0
	// means "no truncation" — byte-identical to the unbounded fusion (dark-
	// launch: prod keeps RRF_RANK_WINDOW=0 until the controller flips it).
	// rrfK=60 is unchanged. Tune via RRF_RANK_WINDOW env.
	RankWindow int
}

// DefaultRRFWeights returns the (1.0, 1.0, 1.0, 1.0, 1.0, 1.0) math identity
// used by tests that don't thread per-deployment config. The deployed env
// defaults for Sparse, Graph, Hotspot, and Recency are 0.0 (dark-launch);
// DefaultRRFWeights() is the math identity, not the rollout policy.
// See defaultRRFWeight* in cmd/go-code/config.go.
func DefaultRRFWeights() RRFWeights {
	return RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 1.0, Graph: 1.0, Hotspot: 1.0, Recency: 1.0}
}

// rrfEntry tracks per-arm membership for a single dedup key in the MergeRRF index.
type rrfEntry struct {
	result     SearchResult
	inSemantic bool
	inKeyword  bool
	inSparse   bool
	inGraph    bool
	inHotspot  bool
	inRecency  bool
}

// buildRRFIndex populates the dedup index and per-arm ID lists from all six arms.
// Extracted to keep MergeRRF below the cyclop threshold (max 15).
func buildRRFIndex(
	semantic []SearchResult, keyword []KeywordHit, sparse []SparseHit,
	graph, hotspot, recency []GraphHit,
) (index map[string]*rrfEntry, semIDs, kwIDs, sparseIDs, graphIDs, hotspotIDs, recencyIDs []string) {
	index = make(map[string]*rrfEntry, len(semantic)+len(keyword)+len(sparse)+len(graph)+len(hotspot)+len(recency))
	semIDs = make([]string, 0, len(semantic))
	kwIDs = make([]string, 0, len(keyword))
	sparseIDs = make([]string, 0, len(sparse))
	graphIDs = make([]string, 0, len(graph))
	hotspotIDs = make([]string, 0, len(hotspot))
	recencyIDs = make([]string, 0, len(recency))

	for _, r := range semantic {
		key := r.FilePath + ":" + r.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: r}
		}
		index[key].inSemantic = true
		semIDs = append(semIDs, key)
	}
	for _, h := range keyword {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: SearchResult{
				FilePath: h.FilePath, SymbolName: h.SymbolName, StartLine: h.Line,
			}}
		}
		index[key].inKeyword = true
		kwIDs = append(kwIDs, key)
	}
	for _, h := range sparse {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: SearchResult{
				FilePath: h.FilePath, SymbolName: h.SymbolName, StartLine: h.Line,
			}}
		}
		index[key].inSparse = true
		sparseIDs = append(sparseIDs, key)
	}
	for _, h := range graph {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: SearchResult{
				FilePath:   h.FilePath,
				SymbolName: h.SymbolName,
				SymbolKind: h.SymbolKind,
				StartLine:  h.Line,
				Source:     sourceGraph,
			}}
		}
		index[key].inGraph = true
		graphIDs = append(graphIDs, key)
	}
	for _, h := range hotspot {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: SearchResult{
				FilePath:   h.FilePath,
				SymbolName: h.SymbolName,
				SymbolKind: h.SymbolKind,
				StartLine:  h.Line,
				Source:     sourceHotspot,
			}}
		}
		index[key].inHotspot = true
		hotspotIDs = append(hotspotIDs, key)
	}
	for _, h := range recency {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := index[key]; !ok {
			index[key] = &rrfEntry{result: SearchResult{
				FilePath:   h.FilePath,
				SymbolName: h.SymbolName,
				SymbolKind: h.SymbolKind,
				StartLine:  h.Line,
				Source:     sourceRecency,
			}}
		}
		index[key].inRecency = true
		recencyIDs = append(recencyIDs, key)
	}
	return index, semIDs, kwIDs, sparseIDs, graphIDs, hotspotIDs, recencyIDs
}

// MergeRRF combines semantic, keyword, sparse, graph, hotspot, and recency
// results using Weighted Reciprocal Rank Fusion. Backed by
// go-kit/rerank.WeightedRRF (Cormack-Clarke 2009 with per-list weights, k=60).
// Key = FilePath + ":" + SymbolName for dedup; results in multiple lists are
// attributed "hybrid". Returns at most topK results.
//
// weights are env-driven (RRF_WEIGHT_SEMANTIC / RRF_WEIGHT_KEYWORD /
// RRF_WEIGHT_SPARSE / RRF_WEIGHT_GRAPH / RRF_WEIGHT_HOTSPOT / RRF_WEIGHT_RECENCY)
// and surfaced via Prometheus gauge gocode_rrf_weights{retriever}.
//
// Dark-launch guarantee: when a weight is 0 OR its list is empty, that arm
// contributes nothing to ranking. A 0-weight arm contributes 1/(k+rank)*0 = 0
// to every doc, so the existing ordering is preserved. This is verified for
// Sparse and Graph by existing tests and applies to Hotspot/Recency as well.
func MergeRRF(
	semantic []SearchResult, keyword []KeywordHit, sparse []SparseHit,
	graph, hotspot, recency []GraphHit, topK int, weights RRFWeights,
) []HybridResult {
	if len(semantic) == 0 && len(keyword) == 0 && len(sparse) == 0 &&
		len(graph) == 0 && len(hotspot) == 0 && len(recency) == 0 {
		return nil
	}

	// rank_window_size cap (issue #663): truncate each arm's ranked input to
	// its top-N BEFORE buildRRFIndex so beyond-window IDs never enter the
	// dedup index from that arm. Sentinel <= 0 = no truncation (byte-identical
	// to the unbounded fusion — dark-launch default). Each arm is already in
	// ranked order (semantic by distance asc, keyword by relevance, etc.), so
	// a prefix slice preserves the top-N. rrfK=60 is unchanged.
	if w := weights.RankWindow; w > 0 {
		semantic = capSearchResults(semantic, w)
		keyword = capKeywordHits(keyword, w)
		sparse = capSparseHits(sparse, w)
		graph = capGraphHits(graph, w)
		hotspot = capGraphHits(hotspot, w)
		recency = capGraphHits(recency, w)
	}

	index, semIDs, kwIDs, sparseIDs, graphIDs, hotspotIDs, recencyIDs :=
		buildRRFIndex(semantic, keyword, sparse, graph, hotspot, recency)

	fused := rerank.WeightedRRF(rrfK,
		[]float64{weights.Semantic, weights.Keyword, weights.Sparse, weights.Graph, weights.Hotspot, weights.Recency},
		semIDs, kwIDs, sparseIDs, graphIDs, hotspotIDs, recencyIDs)

	results := make([]HybridResult, 0, len(fused))
	for _, f := range fused {
		e := index[f.ID]
		if e == nil {
			continue
		}
		results = append(results, HybridResult{
			SearchResult: e.result,
			Source:       attributeSource(e.inSemantic, e.inKeyword, e.inSparse, e.inGraph, e.inHotspot, e.inRecency),
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

// attributeSource maps per-arm membership flags to a source label.
// When a result appears in more than one retrieval arm it is "hybrid";
// otherwise the label matches the single contributing arm.
func attributeSource(inSem, inKw, inSparse, inGraph, inHotspot, inRecency bool) string {
	arms := 0
	if inSem {
		arms++
	}
	if inKw {
		arms++
	}
	if inSparse {
		arms++
	}
	if inGraph {
		arms++
	}
	if inHotspot {
		arms++
	}
	if inRecency {
		arms++
	}
	if arms > 1 {
		return sourceHybrid
	}
	switch {
	case inKw:
		return sourceKeyword
	case inSparse:
		return sourceSparse
	case inGraph:
		return sourceGraph
	case inHotspot:
		return sourceHotspot
	case inRecency:
		return sourceRecency
	default:
		return sourceSemantic
	}
}

// capSearchResults returns xs truncated to its first n entries (the ranked
// top-N) when n > 0 and len(xs) > n; otherwise xs unchanged. Used by the
// rank_window_size cap in MergeRRF. Does not copy when no truncation is needed.
func capSearchResults(xs []SearchResult, n int) []SearchResult {
	if n > 0 && len(xs) > n {
		return xs[:n]
	}
	return xs
}

// capKeywordHits is the []KeywordHit variant of capSearchResults.
func capKeywordHits(xs []KeywordHit, n int) []KeywordHit {
	if n > 0 && len(xs) > n {
		return xs[:n]
	}
	return xs
}

// capSparseHits is the []SparseHit variant of capSearchResults.
func capSparseHits(xs []SparseHit, n int) []SparseHit {
	if n > 0 && len(xs) > n {
		return xs[:n]
	}
	return xs
}

// capGraphHits is the []GraphHit variant of capSearchResults (shared by the
// graph, hotspot, and recency arms, all of which are []GraphHit).
func capGraphHits(xs []GraphHit, n int) []GraphHit {
	if n > 0 && len(xs) > n {
		return xs[:n]
	}
	return xs
}
