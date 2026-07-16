package callgraph

import gocodescip "github.com/anatolykoptev/go-code/internal/scip"

// FilterStdlibCalls removes unresolved call edges whose callee name is a
// known standard-library method/builtin (clone, unwrap, to_string, iter,
// map, len, push, format, …). These edges never resolve to a project node
// and only pollute call traces with unresolved "external" nodes.
//
// Only edges with a nil Callee (unresolved) are filtered — a project
// function that happens to share a name with a stdlib method (e.g. a
// user-defined `Clone`) is kept because its Callee symbol is set.
//
// Applied after EnrichWithTypedResolution in both BuildFromRepo (the
// call_trace/impact_analysis path) and buildAGECallGraph (the code_graph
// indexing path), so the tree-sitter pipeline gets the same stdlib noise
// filter the SCIP conversion path already applies in convert.go. The
// SCIP-level filter is kept too (defence in depth — fewer edges to
// convert). See issue #466.
func FilterStdlibCalls(edges []CallEdge) []CallEdge {
	filtered := edges[:0:0]
	for _, e := range edges {
		if e.Callee == nil && gocodescip.IsStdlibMethod(e.CalleeName) {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}
