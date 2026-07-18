package codegraph

import (
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// callEdgesToRels extracts IsInterface edges from a CallGraph and converts
// them to parser.TypeRelationship values of kind RelImplements. The
// returned rels are ready to flow through buildRelationshipEdges — the
// single unified path for IMPLEMENTS edges in the AGE graph.
//
// The caller should remove the IsInterface edges from cg.Edges after this
// call so they don't also appear as CALLS edges in buildGraph.
func callEdgesToRels(cg *callgraph.CallGraph) []parser.TypeRelationship {
	if cg == nil {
		return nil
	}
	var rels []parser.TypeRelationship
	for _, ce := range cg.Edges {
		if !ce.IsInterface {
			continue
		}
		if ce.Caller == nil || ce.Callee == nil {
			continue
		}
		rels = append(rels, parser.TypeRelationship{
			Subject: ce.Caller.Name,
			Target:  ce.Callee.Name,
			Kind:    parser.RelImplements,
			File:    ce.Caller.File,
			Line:    ce.Line,
		})
	}
	return rels
}

// removeImplEdges returns a copy of cg.Edges with all IsInterface edges
// removed. This prevents IMPLEMENTS edges from also appearing as CALLS
// edges in buildGraph after they've been converted to TypeRelationship.
func removeImplEdges(edges []callgraph.CallEdge) []callgraph.CallEdge {
	if len(edges) == 0 {
		return edges
	}
	filtered := edges[:0]
	for _, ce := range edges {
		if !ce.IsInterface {
			filtered = append(filtered, ce)
		}
	}
	return filtered
}
