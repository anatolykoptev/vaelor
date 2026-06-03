package embeddings

import (
	"context"
	"strings"
)

// methodEndpoint holds a candidate method symbol resolved from the graph, with the
// receiver type parsed from its signature. file+name reconstruct the PairKey;
// receiver is the bare receiver type used for the IMPLEMENTS 2-hop.
type methodEndpoint struct {
	name     string
	file     string
	receiver string
}

// graphHasImplementsEdges reports whether the named graph has any
// (type)-[:IMPLEMENTS]->(interface) edges. It drives the exact-vs-heuristic
// branch in PairsSharingInterface: Go repos reindexed with the go/types
// satisfaction pass have IMPLEMENTS edges (exact path); everything else has zero
// (heuristic fallback).
//
// Any query error or graph-missing returns false → heuristic fallback, preserving
// the never-worse-than-#218 contract.
func (e *Expander) graphHasImplementsEdges(ctx context.Context, graphName string) bool {
	rows := e.execCypherN(ctx, graphName,
		"MATCH ()-[r:IMPLEMENTS]->() RETURN count(r)", "n agtype")
	if len(rows) == 0 || len(rows[0]) == 0 {
		return false
	}
	return strings.TrimSpace(rows[0][0]) != "" && rows[0][0] != "0"
}

// pairsSharingInterfaceExact is the durable 2-hop discriminator over real
// IMPLEMENTS edges. A candidate pair is suppressed (treated as an interface
// sibling) iff both endpoints are methods whose RECEIVER types both implement a
// COMMON interface in the graph:
//
//	(receiverA)-[:IMPLEMENTS]->(i)<-[:IMPLEMENTS]-(receiverB)
//
// This closes the residual false-negative the heuristic could not distinguish:
// two exported same-name methods on DISTINCT receivers that share NO interface are
// now correctly REPORTED (not suppressed), because no common interface node exists.
//
// Receiver types are parsed from each method's signature (the graph has no
// method→receiver-type edge; the receiver lives only in the signature, e.g.
// "func (g *GitHubForge) FetchREADME(...)"). The method name match is implied by
// the pair already being a same-name similar pair; the shared-interface hop is the
// authoritative signal.
func (e *Expander) pairsSharingInterfaceExact(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	names := symbolNamesFromPairs(pairs)
	if len(names) == 0 {
		return map[PairKey]bool{}, nil
	}

	inputSet := make(map[PairKey]bool, len(pairs))
	for _, pk := range pairs {
		inputSet[pk] = true
	}

	// 1. Resolve every candidate method symbol to (name, file, receiver). Only
	//    methods can be interface siblings; free functions are skipped here.
	endpoints := e.resolveMethodEndpoints(ctx, graphName, names)
	if len(endpoints) == 0 {
		return map[PairKey]bool{}, nil
	}

	// 2. Build the set of distinct receiver type names and ask the graph which
	//    receiver-type pairs share a common interface (the 2-hop).
	receivers := distinctReceivers(endpoints)
	if len(receivers) == 0 {
		return map[PairKey]bool{}, nil
	}
	connectedReceivers := e.receiverPairsSharingInterface(ctx, graphName, receivers)
	if len(connectedReceivers) == 0 {
		return map[PairKey]bool{}, nil
	}

	// 3. A candidate pair is a sibling iff both endpoints are methods with the same
	//    name, distinct receivers, and those receivers share an interface.
	return matchSiblingPairs(endpoints, connectedReceivers, inputSet), nil
}

// resolveMethodEndpoints fetches the signature of every same-name method symbol in
// names and returns one methodEndpoint per resolved method (receiver parsed).
// Non-method symbols and signatures without a receiver are dropped.
func (e *Expander) resolveMethodEndpoints(ctx context.Context, graphName string, names []string) []methodEndpoint {
	nameFilter := buildNameFilter("a", names)
	cypher := "MATCH (a:Symbol) WHERE (" + nameFilter + ") AND a.kind = 'method'" +
		" RETURN a.name, a.file, a.signature"
	rows := e.execCypherN(ctx, graphName, cypher, "aname agtype, afile agtype, asig agtype")

	const methodRowCols = 3
	var out []methodEndpoint
	for _, row := range rows {
		if len(row) < methodRowCols {
			continue
		}
		name := stripAgtypeQuotes(row[0])
		file := stripAgtypeQuotes(row[1])
		sig := parseSignature(stripAgtypeQuotes(row[2]))
		if name == "" || file == "" || !sig.isMethod || sig.receiver == "" {
			continue
		}
		out = append(out, methodEndpoint{name: name, file: file, receiver: sig.receiver})
	}
	return out
}

// distinctReceivers returns the unique set of receiver type names across endpoints.
func distinctReceivers(endpoints []methodEndpoint) []string {
	seen := make(map[string]bool, len(endpoints))
	var out []string
	for _, ep := range endpoints {
		if !seen[ep.receiver] {
			seen[ep.receiver] = true
			out = append(out, ep.receiver)
		}
	}
	return out
}

// receiverPairsSharingInterface runs the IMPLEMENTS 2-hop and returns the set of
// unordered receiver-type-name pairs that share at least one common interface.
// Keys are canonical "min|max" of the two receiver names.
func (e *Expander) receiverPairsSharingInterface(ctx context.Context, graphName string, receivers []string) map[string]bool {
	const receiverPairCols = 2
	taFilter := buildNameFilter("ta", receivers)
	tbFilter := buildNameFilter("tb", receivers)
	cypher := "MATCH (ta:Symbol)-[:IMPLEMENTS]->(i:Symbol)<-[:IMPLEMENTS]-(tb:Symbol)" +
		" WHERE (" + taFilter + ") AND (" + tbFilter + ") AND ta.name <> tb.name" +
		" RETURN ta.name, tb.name"
	rows := e.execCypherN(ctx, graphName, cypher, "ta agtype, tb agtype")

	connected := make(map[string]bool)
	for _, row := range rows {
		if len(row) < receiverPairCols {
			continue
		}
		ta := stripAgtypeQuotes(row[0])
		tb := stripAgtypeQuotes(row[1])
		if ta == "" || tb == "" || ta == tb {
			continue
		}
		connected[receiverPairKey(ta, tb)] = true
	}
	return connected
}

// receiverPairKey returns a canonical unordered key for two receiver type names.
func receiverPairKey(a, b string) string {
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// matchSiblingPairs intersects the candidate input pairs with the connected
// receiver set: a pair is a sibling iff both endpoints are methods of the same
// name on DISTINCT receivers that share an interface. Endpoints are grouped by
// method name so only same-name method pairs are considered.
func matchSiblingPairs(endpoints []methodEndpoint, connectedReceivers map[string]bool, inputSet map[PairKey]bool) map[PairKey]bool {
	byName := make(map[string][]methodEndpoint)
	for _, ep := range endpoints {
		byName[ep.name] = append(byName[ep.name], ep)
	}

	siblings := make(map[PairKey]bool)
	for _, group := range byName {
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a.receiver == b.receiver {
					continue // same receiver type — not a cross-type sibling
				}
				if !connectedReceivers[receiverPairKey(a.receiver, b.receiver)] {
					continue // receivers share no interface — real duplicate, report it
				}
				pk := NewPairKey(a.file, a.name, b.file, b.name)
				if inputSet[pk] {
					siblings[pk] = true
				}
			}
		}
	}
	return siblings
}
