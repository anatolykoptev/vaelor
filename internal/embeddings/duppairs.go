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
// Why not IMPLEMENTS edges: Go interface satisfaction is structural/type-level —
// there is no `implements` keyword for tree-sitter to capture, so the codegraph
// indexer emits ZERO IMPLEMENTS edges for Go (verified on the go-code self-index:
// IMPLEMENTS=0 while CALLS=11934). The original (a)-[:IMPLEMENTS]->(i)<-[:IMPLEMENTS]-(b)
// Cypher was therefore identically empty — a dead filter. The real IMPLEMENTS-edge
// population via go/types is tracked as a follow-up (see docs); until then this
// method uses a signature-receiver discriminator on the Symbol vertices.
//
// Discriminator: a pair is an interface sibling when both endpoints are methods,
// share the same method name + identical receiver-stripped signature, and sit on
// DISTINCT receiver types. Free functions (no receiver) never match, so genuine
// cross-package reinvention of same-named free functions (countSourceFiles,
// commonPrefixLen) is preserved.
//
// The Cypher matches same-name Symbol-vertex pairs (no edge traversal) and returns
// each endpoint's file/kind/signature; the discriminator runs in Go. AGE returns
// both (a,b) and (b,a) orderings plus a==b self-matches; canonical PairKey (A<=B)
// deduplicates them and the discriminator's distinct-receiver requirement skips
// self-matches.
//
// Same graceful-degradation contract as PairsConnectedByCalls.
func (e *Expander) PairsSharingInterface(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	if len(pairs) == 0 {
		return map[PairKey]bool{}, nil
	}

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
		if !isInterfaceSiblingPair(aName, parseSignature(aSig), bName, parseSignature(bSig)) {
			continue
		}
		pk := NewPairKey(aFile, aName, bFile, bName)
		if inputSet[pk] {
			siblings[pk] = true
		}
	}
	return siblings, nil
}
