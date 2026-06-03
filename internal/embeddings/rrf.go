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

// GraphHit is a single graph-candidate result produced by the graph arm
// (high-PageRank + term match, 2-hop CALLS neighbors, or same-community members).
// Shape is symmetric with KeywordHit/SparseHit for uniform MergeRRF wiring.
// Source is always "graph" before fusion.
type GraphHit struct {
	FilePath   string
	SymbolName string
	SymbolKind string
	Line       int // start_line from the index row; 0 when unknown
}

// Source constants for HybridResult.Source attribution.
const (
	sourceSemantic = "semantic"
	sourceKeyword  = "keyword"
	sourceSparse   = "sparse"
	sourceGraph    = "graph"
	sourceHybrid   = "hybrid"
)

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source   string  // "semantic", "keyword", "sparse", "graph", or "hybrid"
	RRFScore float64 // combined reciprocal rank fusion score
}

// RRFWeights are per-retriever weights for the WeightedRRF fusion in MergeRRF.
// All fields must be ≥ 0 (negative values panic in go-kit/rerank.WeightedRRF
// — programmer error, not runtime). Weights == (1.0, 1.0, 0.0, 0.0) preserves
// byte-identical rollback to the pre-SPLADE 2-arm baseline: the Sparse and Graph
// arms are DARK-LAUNCHED at 0.0 by default.
// Weights == (1.0, 1.0) in the 2-arm sense is mathematically identical to
// plain RRF (Cormack-Clarke 2009).
type RRFWeights struct {
	Semantic float64
	Keyword  float64
	// Sparse is the per-list weight for the SPLADE sparse-retrieval arm.
	// Default env value: 0.0 (dark-launched — plumbed but inert until A/B
	// in Phase 6 clears the gate). Post-A/B recommended value: 0.2–0.4
	// (below dense per research note in 2026-06-01 SPLADE landscape report).
	Sparse float64
	// Graph is the per-list weight for the graph-candidate arm (Phase 1 graph-first
	// retrieval plan). Default env value: 0.0 (dark-launched — byte-identical until
	// A/B clears the gate). See RRF_WEIGHT_GRAPH in cmd/go-code/config.go.
	// Post-A/B recommended band: 0.2–0.4 (below dense, per graph-first plan ADR).
	Graph float64
}

// DefaultRRFWeights returns the (1.0, 1.0, 1.0, 1.0) math identity used by tests
// that don't thread per-deployment config. The deployed env defaults for Sparse
// and Graph are 0.0 (dark-launch); DefaultRRFWeights() is the math identity, not
// the rollout policy. See defaultRRFWeightSparse/Graph in cmd/go-code/config.go.
func DefaultRRFWeights() RRFWeights {
	return RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 1.0, Graph: 1.0}
}

// rrfEntry tracks per-arm membership for a single dedup key in the MergeRRF index.
type rrfEntry struct {
	result     SearchResult
	inSemantic bool
	inKeyword  bool
	inSparse   bool
	inGraph    bool
}

// buildRRFIndex populates the dedup index and per-arm ID lists from all four arms.
// Extracted to keep MergeRRF below the cyclop threshold (max 15).
func buildRRFIndex(
	semantic []SearchResult, keyword []KeywordHit, sparse []SparseHit, graph []GraphHit,
) (index map[string]*rrfEntry, semIDs, kwIDs, sparseIDs, graphIDs []string) {
	index = make(map[string]*rrfEntry, len(semantic)+len(keyword)+len(sparse)+len(graph))
	semIDs = make([]string, 0, len(semantic))
	kwIDs = make([]string, 0, len(keyword))
	sparseIDs = make([]string, 0, len(sparse))
	graphIDs = make([]string, 0, len(graph))

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
	return index, semIDs, kwIDs, sparseIDs, graphIDs
}

// MergeRRF combines semantic, keyword, sparse, and graph results using Weighted
// Reciprocal Rank Fusion. Backed by go-kit/rerank.WeightedRRF (Cormack-Clarke
// 2009 with per-list weights, k=60). Key = FilePath + ":" + SymbolName for
// dedup; results in multiple lists are attributed "hybrid". Returns at most
// topK results.
//
// weights are env-driven (RRF_WEIGHT_SEMANTIC / RRF_WEIGHT_KEYWORD /
// RRF_WEIGHT_SPARSE / RRF_WEIGHT_GRAPH) and surfaced via Prometheus gauge
// gocode_rrf_weights{retriever}.
//
// Dark-launch guarantee: when weights.Sparse == 0 OR sparse is empty, the fused
// output is BYTE-IDENTICAL to the prior result. A 0-weight arm contributes
// 1/(k+rank)*0 = 0 to every doc, so the existing ordering is preserved.
// Same guarantee applies to the Graph arm when weights.Graph == 0.
// Verified by TestMergeRRF_EmptySparseArmIdentical and
// TestMergeRRF_EmptyGraphArmIdentical.
func MergeRRF(semantic []SearchResult, keyword []KeywordHit, sparse []SparseHit, graph []GraphHit, topK int, weights RRFWeights) []HybridResult {
	if len(semantic) == 0 && len(keyword) == 0 && len(sparse) == 0 && len(graph) == 0 {
		return nil
	}

	index, semIDs, kwIDs, sparseIDs, graphIDs := buildRRFIndex(semantic, keyword, sparse, graph)

	fused := rerank.WeightedRRF(rrfK,
		[]float64{weights.Semantic, weights.Keyword, weights.Sparse, weights.Graph},
		semIDs, kwIDs, sparseIDs, graphIDs)

	results := make([]HybridResult, 0, len(fused))
	for _, f := range fused {
		e := index[f.ID]
		if e == nil {
			continue
		}
		results = append(results, HybridResult{
			SearchResult: e.result,
			Source:       attributeSource(e.inSemantic, e.inKeyword, e.inSparse, e.inGraph),
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
func attributeSource(inSem, inKw, inSparse, inGraph bool) string {
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
	default:
		return sourceSemantic
	}
}
