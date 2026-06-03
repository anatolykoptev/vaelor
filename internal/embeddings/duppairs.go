package embeddings

import (
	"context"
	"strings"
)

// pairEdgeCols is the number of columns returned by the CALLS and IMPLEMENTS
// pair-edge Cypher queries: a.name, a.file, b.name, b.file.
const pairEdgeCols = 4

// PairKey identifies an unordered symbol pair by "file_path:symbol_name" endpoints,
// canonicalized so A <= B (matching the store's unique-pair ordering convention used
// by FindSimilarPairs and FindExactDuplicates).
type PairKey struct {
	// A and B are "file_path:symbol_name" endpoint strings, A <= B lexicographically.
	A, B string
}

// NewPairKey builds a canonical PairKey from two (file, symbol) endpoints.
// The endpoints are combined as "file:symbol" and sorted so A <= B, ensuring
// that (file1, sym1, file2, sym2) and (file2, sym2, file1, sym1) produce the
// same key — matching the store's "(a.file:a.sym) < (b.file:b.sym)" ordering.
func NewPairKey(file1, sym1, file2, sym2 string) PairKey {
	ea := file1 + ":" + sym1
	eb := file2 + ":" + sym2
	if ea <= eb {
		return PairKey{A: ea, B: eb}
	}
	return PairKey{A: eb, B: ea}
}

// symbolNameFromEndpoint extracts the symbol name from a "file:symbol" endpoint
// by splitting on the last colon. If there is no colon the whole string is returned.
// This handles method receiver paths like "a.go:Recv:Method" correctly.
func symbolNameFromEndpoint(endpoint string) string {
	idx := strings.LastIndex(endpoint, ":")
	if idx < 0 {
		return endpoint
	}
	return endpoint[idx+1:]
}

// symbolNamesFromPairs collects the unique set of symbol names across all pair
// endpoints. Used to build the Cypher name-filter for batch graph queries.
func symbolNamesFromPairs(pairs []PairKey) []string {
	seen := make(map[string]bool, len(pairs)*2)
	for _, pk := range pairs {
		if n := symbolNameFromEndpoint(pk.A); n != "" {
			seen[n] = true
		}
		if n := symbolNameFromEndpoint(pk.B); n != "" {
			seen[n] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	return names
}

// PairsConnectedByCalls returns the subset of pairs where the two endpoints have a
// CALLS edge between them in either direction within the AGE graph.
//
// The method issues a single Cypher query that matches both (a)-[:CALLS]->(b) and
// (b)-[:CALLS]->(a) orderings by filtering on the combined name set of all pair
// endpoints and checking membership in the input set after reconstructing the
// canonical PairKey from each returned edge.
//
// Graph-missing or any query error → returns an empty map and nil error. The caller
// must not treat graph-down as fatal; graceful degradation is a correctness requirement
// (a graph hiccup must not hide real duplicates by silently removing nothing).
func (e *Expander) PairsConnectedByCalls(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	if len(pairs) == 0 {
		return map[PairKey]bool{}, nil
	}

	names := symbolNamesFromPairs(pairs)
	if len(names) == 0 {
		return map[PairKey]bool{}, nil
	}

	// Build an input set for O(1) membership check.
	inputSet := make(map[PairKey]bool, len(pairs))
	for _, pk := range pairs {
		inputSet[pk] = true
	}

	nameFilterA := buildNameFilter("a", names)
	nameFilterB := buildNameFilter("b", names)
	cypher := "MATCH (a)-[:CALLS]->(b) WHERE (" + nameFilterA + ") AND (" + nameFilterB + ") RETURN a.name, a.file, b.name, b.file"

	rows := e.execCypherN(ctx, graphName, cypher, "aname agtype, afile agtype, bname agtype, bfile agtype")

	connected := make(map[PairKey]bool)
	for _, row := range rows {
		if len(row) < pairEdgeCols {
			continue
		}
		aName := stripAgtypeQuotes(row[0])
		aFile := stripAgtypeQuotes(row[1])
		bName := stripAgtypeQuotes(row[2])
		bFile := stripAgtypeQuotes(row[3])
		if aName == "" || aFile == "" || bName == "" || bFile == "" {
			continue
		}
		pk := NewPairKey(aFile, aName, bFile, bName)
		if inputSet[pk] {
			connected[pk] = true
		}
	}
	return connected, nil
}

// PairsSharingInterface returns the subset of pairs that are interface-impl
// siblings rather than refactor-worthy duplicates.
//
// Interface-sibling pairs are the largest false-positive class: multiple concrete
// types implementing the same interface (e.g. *GitHubForge.FetchREADME vs
// *GitLabForge.FetchREADME, or four Search methods) look semantically identical
// but are distinct correct implementations, not duplicates.
//
// Two discriminator paths, selected per call:
//
//   - EXACT (preferred): when the graph has real (type)-[:IMPLEMENTS]->(interface)
//     edges (a Go repo reindexed with the go/types satisfaction pass), a pair is a
//     sibling iff both methods' receiver types implement a COMMON interface. This is
//     the durable 2-hop fix and closes the residual false-negative the heuristic
//     could not: two exported same-name methods on DISTINCT receivers that share NO
//     interface are now correctly REPORTED, not suppressed.
//
//   - HEURISTIC (fallback): when the graph has ZERO IMPLEMENTS edges (non-Go repos,
//     or a Go repo not yet reindexed after the indexer change), it falls back to the
//     #218 signature-receiver discriminator. Degradation is graceful — never worse
//     than #218.
//
// Same graceful-degradation contract as PairsConnectedByCalls: any graph error
// yields an empty map and nil error (a hiccup must not hide real duplicates).
func (e *Expander) PairsSharingInterface(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	if len(pairs) == 0 {
		return map[PairKey]bool{}, nil
	}

	if e.graphHasImplementsEdges(ctx, graphName) {
		interfaceSiblingPathTotal.WithLabelValues(ifacePathExact).Inc()
		return e.pairsSharingInterfaceExact(ctx, graphName, pairs)
	}
	interfaceSiblingPathTotal.WithLabelValues(ifacePathHeuristic).Inc()
	return e.pairsSharingInterfaceHeuristic(ctx, graphName, pairs)
}

// pairsSharingInterfaceHeuristic is the #218 signature-receiver discriminator,
// retained as the fallback when no IMPLEMENTS edges exist for the graph.
//
// Discriminator: a pair is an interface sibling when both endpoints are methods,
// share the same method name + identical receiver-stripped signature, and sit on
// DISTINCT receiver types. Free functions (no receiver) never match, so genuine
// cross-package reinvention of same-named free functions (countSourceFiles,
// commonPrefixLen) is preserved. An UNEXPORTED method on receivers in DIFFERENT
// packages is also kept (reported): no interface can name unexported methods
// from two packages, so such a pair is provably real copy-paste, not a sibling
// (see isInterfaceSiblingPair).
//
// The Cypher matches same-name Symbol-vertex pairs (no edge traversal) and returns
// each endpoint's file/kind/signature; the discriminator runs in Go. AGE returns
// both (a,b) and (b,a) orderings plus a==b self-matches; canonical PairKey (A<=B)
// deduplicates them and the discriminator's distinct-receiver requirement skips
// self-matches.
func (e *Expander) pairsSharingInterfaceHeuristic(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	names := symbolNamesFromPairs(pairs)
	if len(names) == 0 {
		return map[PairKey]bool{}, nil
	}

	inputSet := make(map[PairKey]bool, len(pairs))
	for _, pk := range pairs {
		inputSet[pk] = true
	}

	nameFilterA := buildNameFilter("a", names)
	nameFilterB := buildNameFilter("b", names)
	cypher := "MATCH (a:Symbol), (b:Symbol) WHERE (" + nameFilterA + ") AND (" + nameFilterB + ")" +
		" AND a.kind = 'method' AND b.kind = 'method'" +
		" RETURN a.name, a.file, a.kind, a.signature, b.name, b.file, b.kind, b.signature"

	rows := e.execCypherN(ctx, graphName, cypher,
		"aname agtype, afile agtype, akind agtype, asig agtype, bname agtype, bfile agtype, bkind agtype, bsig agtype")

	siblings := make(map[PairKey]bool)
	for _, row := range rows {
		if len(row) < ifacePairCols {
			continue
		}
		aName := stripAgtypeQuotes(row[0])
		aFile := stripAgtypeQuotes(row[1])
		aSig := stripAgtypeQuotes(row[3])
		bName := stripAgtypeQuotes(row[4])
		bFile := stripAgtypeQuotes(row[5])
		bSig := stripAgtypeQuotes(row[7])
		if aName == "" || aFile == "" || bName == "" || bFile == "" {
			continue
		}
		// Skip self-matches (a and b resolved to the same endpoint).
		if aFile == bFile && aName == bName {
			continue
		}
		if !isInterfaceSiblingPair(aName, aFile, parseSignature(aSig), bName, bFile, parseSignature(bSig)) {
			continue
		}
		pk := NewPairKey(aFile, aName, bFile, bName)
		if inputSet[pk] {
			siblings[pk] = true
		}
	}
	return siblings, nil
}
