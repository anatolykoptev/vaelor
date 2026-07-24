package codegraph

import (
	"math"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/ranking"
)

const (
	// pagerankIterations is the iteration count for the unpersonalized fallback
	// path. Preserved at 20 so the nil-seed result is byte-identical to the
	// pre-PPR behaviour — no regression when there is no query context.
	pagerankIterations = 20
	// personalizedIterations is the iteration count for the query-seeded PPR
	// path. Raised from 20 to 40 for convergence on larger graphs (Aider/NetworkX
	// default 100; 40 is a convergence/latency balance).
	personalizedIterations = 40
	pagerankDamping        = 0.85

	// seedPersonalizationWeight is the personalization weight assigned to each
	// query-matched candidate symbol (Aider's chat-file multiplier ~50). The
	// value is normalised inside WeightedPersonalizedPageRank, so only the
	// relative magnitude matters; 50 documents the Aider convention.
	seedPersonalizationWeight = 50.0

	// Aider repomap.py edge-weight heuristics.
	commonSymbolThreshold = 5    // >5 definitions → common-name discount.
	privateMultiplier     = 0.1  // underscore-prefixed names.
	commonMultiplier      = 0.1  // names defined in >5 places.
	seedMatchMultiplier   = 10.0 // callee matching the query/seed set.
)

// computeSymbolPageRank runs PageRank on the CALLS graph and returns scores
// keyed by symbol key ("Name:relFile").
//
// When seeds is non-empty, it runs query-seeded WeightedPersonalizedPageRank
// over a weighted CALLS graph whose edge weights apply the Aider repomap.py
// heuristics: sqrt(num_refs) damping, private (_prefix) discount, common-name
// (>5 definitions) discount, and a seed-match boost. This ranks query-relevant
// symbols and their neighbourhood above globally-central infrastructure.
//
// When seeds is empty (no query context — the index-time buildGraph call), it
// falls back to unpersonalized unweighted PageRank with the original 20
// iterations, producing byte-identical scores to the pre-PPR behaviour so the
// persisted s.pagerank never regresses.
func computeSymbolPageRank(
	root string,
	symbols []*parser.Symbol,
	cg *callgraph.CallGraph,
	seeds map[string]float64,
) map[string]float64 {
	if len(cg.Edges) == 0 {
		return nil
	}

	graph := buildUnweightedCallGraph(root, symbols, cg)

	// Empty-seed fallback: unpersonalized, unweighted — identical to pre-PPR.
	if len(seeds) == 0 {
		return ranking.PageRank(graph, pagerankIterations, pagerankDamping)
	}

	// PPR path: weighted edges with Aider heuristics, query-seeded.
	weighted := buildWeightedCallGraph(root, symbols, cg, seeds)
	return ranking.WeightedPersonalizedPageRank(weighted, seeds, personalizedIterations, pagerankDamping)
}

// buildUnweightedCallGraph builds the directed CALLS graph (caller→[]callees)
// keyed by symbol key ("Name:relFile"). All symbols are included as nodes even
// when they have no edges. Shared by the fallback path and tests.
func buildUnweightedCallGraph(root string, symbols []*parser.Symbol, cg *callgraph.CallGraph) map[string][]string {
	graph := make(map[string][]string)
	for _, edge := range cg.Edges {
		if edge.Caller == nil || edge.Callee == nil {
			continue
		}
		callerKey := edge.Caller.Name + compositeKeyDelim + relPath(edge.Caller.File, root)
		calleeKey := edge.Callee.Name + compositeKeyDelim + relPath(edge.Callee.File, root)
		graph[callerKey] = append(graph[callerKey], calleeKey)
	}
	for _, sym := range symbols {
		key := sym.Name + compositeKeyDelim + relPath(sym.File, root)
		if _, ok := graph[key]; !ok {
			graph[key] = nil
		}
	}
	return graph
}

// buildWeightedCallGraph builds a weighted directed CALLS graph
// (src→dst→weight) applying the Aider repomap.py edge-weight heuristics:
//
//	weight = sqrt(num_refs) * privateMul * commonMul * seedMul
//
// where num_refs is the callee's inbound CALLS reference count (dampens
// high-frequency reference edges so a 100-ref edge dominates a 4-ref edge
// 5:1, not 25:1), privateMul discounts underscore-prefixed names, commonMul
// discounts names defined in more than commonSymbolThreshold places, and
// seedMul boosts edges into query-matched symbols. All symbols are included
// as nodes even when they have no edges.
func buildWeightedCallGraph(
	root string,
	symbols []*parser.Symbol,
	cg *callgraph.CallGraph,
	seeds map[string]float64,
) map[string]map[string]float64 {
	// Inbound reference count per callee key (Aider's num_refs).
	numRefs := make(map[string]int)
	for _, edge := range cg.Edges {
		if edge.Callee == nil {
			continue
		}
		k := edge.Callee.Name + compositeKeyDelim + relPath(edge.Callee.File, root)
		numRefs[k]++
	}

	// Definition count per symbol name (common-name detection).
	defCount := make(map[string]int)
	for _, sym := range symbols {
		defCount[sym.Name]++
	}

	weighted := make(map[string]map[string]float64)
	// Ensure all symbols are nodes (even with no edges).
	for _, sym := range symbols {
		k := sym.Name + compositeKeyDelim + relPath(sym.File, root)
		if _, ok := weighted[k]; !ok {
			weighted[k] = make(map[string]float64)
		}
	}

	for _, edge := range cg.Edges {
		if edge.Caller == nil || edge.Callee == nil {
			continue
		}
		callerKey := edge.Caller.Name + compositeKeyDelim + relPath(edge.Caller.File, root)
		calleeKey := edge.Callee.Name + compositeKeyDelim + relPath(edge.Callee.File, root)

		refs := numRefs[calleeKey]
		if refs < 1 {
			refs = 1
		}
		w := math.Sqrt(float64(refs))

		calleeName := edge.Callee.Name
		if strings.HasPrefix(calleeName, "_") {
			w *= privateMultiplier
		}
		if defCount[calleeName] > commonSymbolThreshold {
			w *= commonMultiplier
		}
		if _, ok := seeds[calleeKey]; ok {
			w *= seedMatchMultiplier
		}

		if weighted[callerKey] == nil {
			weighted[callerKey] = make(map[string]float64)
		}
		// Sum weights for duplicate caller→callee edges (multi-call sites).
		weighted[callerKey][calleeKey] += w
	}

	return weighted
}
