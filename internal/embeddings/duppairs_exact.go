package embeddings

import (
	"context"
	"path"
	"strconv"
	"strings"
)

// receiverIDSep separates the package directory from the type name in a qualified
// receiver identity ("internal/cache\x00Cache"). A NUL byte is used because it can
// never appear in a repo-relative path or a Go identifier, making the qualification
// unambiguous. It mirrors codegraph.compositeKeyDelim (kept local — that constant is
// unexported in another package and the qualified id never crosses a package boundary).
const receiverIDSep = "\x00"

// methodEndpoint holds a candidate method symbol resolved from the graph, with the
// receiver type parsed from its signature. file+name reconstruct the PairKey;
// receiver is the bare receiver type name and receiverID is the package-qualified
// receiver identity used for the IMPLEMENTS 2-hop, so two DISTINCT same-named types
// in different packages are never cross-connected.
type methodEndpoint struct {
	name       string
	file       string
	receiver   string // bare receiver type name, e.g. "Cache"
	receiverID string // package-qualified identity, e.g. "internal/cache\x00Cache"
}

// receiverID builds the package-qualified identity of a receiver type. The package
// is the directory of the source file that declares the symbol (Go enforces one
// package per directory), and a method's receiver type is always declared in the
// method's own package — so the method's file directory identifies which package's
// type "name" the receiver resolves to. This is the same package discriminator used
// by differentPackage (path.Dir on the repo-relative, forward-slash graph file path).
func receiverID(file, typeName string) string {
	return path.Dir(file) + receiverIDSep + typeName
}

// graphHasImplementsEdges reports whether the named graph has any
// (type)-[:IMPLEMENTS]->(interface) edges. It drives the exact-vs-heuristic
// branch in PairsSharingInterface: Go repos reindexed with the go/types
// satisfaction pass have IMPLEMENTS edges (exact path); everything else has zero
// (heuristic fallback).
//
// Any query error or graph-missing returns false → heuristic fallback, preserving
// the never-worse-than-#218 contract. The count agtype is rendered by AGE as a
// (possibly space-padded, possibly quoted) string; it is parsed via the same
// strconv.Atoi(strings.Trim(...,"\"")) path as internal/codegraph/store_helpers.go
// so a "0" rendering can never be misread as "has edges".
func (e *Expander) graphHasImplementsEdges(ctx context.Context, graphName string) bool {
	rows := e.execCypherN(ctx, graphName,
		"MATCH ()-[r:IMPLEMENTS]->() RETURN count(r)", "n agtype")
	if len(rows) == 0 || len(rows[0]) == 0 {
		return false
	}
	n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(rows[0][0]), `"`))
	return err == nil && n > 0
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
//
// Receiver identity is package-qualified (receiverID): two distinct types with the
// SAME bare name in DIFFERENT packages (e.g. a Cache in internal/parser vs a Cache
// in internal/cache) are never cross-connected. Without qualification, one
// same-named pair sharing an interface would suppress an unrelated same-named pair
// that shares none — re-introducing the over-suppression class this arc eliminates.
func (e *Expander) pairsSharingInterfaceExact(ctx context.Context, graphName string, pairs []PairKey) (map[PairKey]bool, error) {
	names := symbolNamesFromPairs(pairs)
	if len(names) == 0 {
		return map[PairKey]bool{}, nil
	}

	inputSet := make(map[PairKey]bool, len(pairs))
	for _, pk := range pairs {
		inputSet[pk] = true
	}

	// 1. Resolve every candidate method symbol to (name, file, receiver, receiverID).
	//    Only methods can be interface siblings; free functions are skipped here.
	endpoints := e.resolveMethodEndpoints(ctx, graphName, names)
	if len(endpoints) == 0 {
		return map[PairKey]bool{}, nil
	}

	// 2. Build the set of distinct receiver type NAMES (for the Cypher name-filter)
	//    and ask the graph which receiver-type pairs share a common interface. The
	//    2-hop returns each receiver type with its FILE, so the result is keyed on
	//    the package-qualified identity, not the bare name.
	receivers := distinctReceiverNames(endpoints)
	if len(receivers) == 0 {
		return map[PairKey]bool{}, nil
	}
	connectedReceivers := e.receiverPairsSharingInterface(ctx, graphName, receivers)
	if len(connectedReceivers) == 0 {
		return map[PairKey]bool{}, nil
	}

	// 3. A candidate pair is a sibling iff both endpoints are methods with the same
	//    name, distinct (package-qualified) receivers, and those receivers share an
	//    interface.
	return matchSiblingPairs(endpoints, connectedReceivers, inputSet), nil
}

// resolveMethodEndpoints fetches the signature of every same-name method symbol in
// names and returns one methodEndpoint per resolved method (receiver parsed). The
// receiverID is derived from the method's own file directory, so the receiver
// resolves to its OWN package's type — never a same-named type elsewhere.
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
		out = append(out, methodEndpoint{
			name:       name,
			file:       file,
			receiver:   sig.receiver,
			receiverID: receiverID(file, sig.receiver),
		})
	}
	return out
}

// distinctReceiverNames returns the unique set of bare receiver type names across
// endpoints. Bare names (not qualified ids) are correct here: they feed the Cypher
// name-filter, and AGE matches type nodes by name; the package disambiguation
// happens after, when the 2-hop returns each type's file.
func distinctReceiverNames(endpoints []methodEndpoint) []string {
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
// unordered receiver-type PAIRS that share at least one common interface. The query
// returns each receiver type's name AND file, so a pair is keyed on the
// package-qualified identity (receiverID) of both ends — two same-named types in
// different packages produce different keys and are never cross-connected.
func (e *Expander) receiverPairsSharingInterface(ctx context.Context, graphName string, receivers []string) map[string]bool {
	const receiverPairCols = 4
	taFilter := buildNameFilter("ta", receivers)
	tbFilter := buildNameFilter("tb", receivers)
	cypher := "MATCH (ta:Symbol)-[:IMPLEMENTS]->(i:Symbol)<-[:IMPLEMENTS]-(tb:Symbol)" +
		" WHERE (" + taFilter + ") AND (" + tbFilter + ")" +
		" RETURN ta.name, ta.file, tb.name, tb.file"
	rows := e.execCypherN(ctx, graphName, cypher, "taname agtype, tafile agtype, tbname agtype, tbfile agtype")

	connected := make(map[string]bool)
	for _, row := range rows {
		if len(row) < receiverPairCols {
			continue
		}
		taName := stripAgtypeQuotes(row[0])
		taFile := stripAgtypeQuotes(row[1])
		tbName := stripAgtypeQuotes(row[2])
		tbFile := stripAgtypeQuotes(row[3])
		if taName == "" || taFile == "" || tbName == "" || tbFile == "" {
			continue
		}
		idA := receiverID(taFile, taName)
		idB := receiverID(tbFile, tbName)
		if idA == idB {
			continue // same package-qualified type (incl. AGE self-match) — not a cross-type pair
		}
		connected[receiverPairKey(idA, idB)] = true
	}
	return connected
}

// receiverPairKey returns a canonical unordered key for two package-qualified
// receiver identities.
func receiverPairKey(a, b string) string {
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// matchSiblingPairs intersects the candidate input pairs with the connected
// receiver set: a pair is a sibling iff both endpoints are methods of the same
// name on DISTINCT (package-qualified) receivers that share an interface. Endpoints
// are grouped by method name so only same-name method pairs are considered, and the
// connected-set lookup uses each endpoint's package-qualified receiverID — so a real
// duplicate pair on two same-named types in different packages is REPORTED even when
// some OTHER same-named type pair shares an interface.
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
				if a.receiverID == b.receiverID {
					continue // same package-qualified receiver — not a cross-type sibling
				}
				if !connectedReceivers[receiverPairKey(a.receiverID, b.receiverID)] {
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
