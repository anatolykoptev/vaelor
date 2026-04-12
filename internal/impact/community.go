// Package impact computes blast radius for changing a symbol.
package impact

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

// buildCommunityMap runs Louvain on the call graph and returns symbol → community ID.
func buildCommunityMap(cg *callgraph.CallGraph) map[*parser.Symbol]int {
	graph := make(map[string]map[string]int)
	symByKey := make(map[string]*parser.Symbol)

	for _, sym := range cg.Symbols {
		key := sym.Name + ":" + sym.File
		if graph[key] == nil {
			graph[key] = make(map[string]int)
		}
		symByKey[key] = sym
	}
	for _, edge := range cg.Edges {
		if edge.Caller == nil || edge.Callee == nil {
			continue
		}
		from := edge.Caller.Name + ":" + edge.Caller.File
		to := edge.Callee.Name + ":" + edge.Callee.File
		if from == to {
			continue
		}
		if graph[from] == nil {
			graph[from] = make(map[string]int)
		}
		graph[from][to]++
		if graph[to] == nil {
			graph[to] = make(map[string]int)
		}
		graph[to][from]++
	}

	communities := ranking.LouvainWeighted(graph)
	if communities == nil {
		return nil
	}

	result := make(map[*parser.Symbol]int, len(communities))
	for key, comm := range communities {
		if sym, ok := symByKey[key]; ok {
			result[sym] = comm
		}
	}
	return result
}
