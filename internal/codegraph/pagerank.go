package codegraph

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

const (
	pagerankIterations = 20
	pagerankDamping    = 0.85
)

// computeSymbolPageRank runs PageRank on the CALLS graph and returns scores
// keyed by symbol key ("Name:relFile").
func computeSymbolPageRank(root string, symbols []*parser.Symbol, cg *callgraph.CallGraph) map[string]float64 {
	if len(cg.Edges) == 0 {
		return nil
	}

	// Build directed graph: caller → []callees (using symbol keys).
	graph := make(map[string][]string)
	for _, edge := range cg.Edges {
		if edge.Caller == nil || edge.Callee == nil {
			continue
		}
		callerKey := edge.Caller.Name + compositeKeyDelim + relPath(edge.Caller.File, root)
		calleeKey := edge.Callee.Name + compositeKeyDelim + relPath(edge.Callee.File, root)
		graph[callerKey] = append(graph[callerKey], calleeKey)
	}

	// Ensure all symbols are nodes (even if they have no edges).
	for _, sym := range symbols {
		key := sym.Name + compositeKeyDelim + relPath(sym.File, root)
		if _, ok := graph[key]; !ok {
			graph[key] = nil
		}
	}

	return ranking.PageRank(graph, pagerankIterations, pagerankDamping)
}
