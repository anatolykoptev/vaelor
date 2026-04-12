package codegraph

import (
	"strconv"

	"github.com/anatolykoptev/go-code/internal/ranking"
)

// injectCommunities runs Louvain community detection on Symbol-level edges
// and sets the "community" property on each Symbol vertex in-place.
// Must be called after buildGraph and before insertBatches.
func injectCommunities(vertices []vertexData, edges []edgeData) {
	// Build undirected adjacency from CALLS + INHERITS + IMPLEMENTS + TESTED_BY edges
	// between Symbols (skip CONTAINS — those are structural, not semantic).
	graph := make(map[string][]string)

	// Ensure all symbols are nodes (even if isolated).
	for _, v := range vertices {
		if v.Label == "Symbol" {
			key := v.Props["name"] + ":" + v.Props["file"]
			if _, ok := graph[key]; !ok {
				graph[key] = nil
			}
		}
	}

	semanticEdges := map[string]bool{
		"CALLS":      true,
		"INHERITS":   true,
		"IMPLEMENTS": true,
		"TESTED_BY":  true,
	}

	for _, e := range edges {
		if e.FromLabel != "Symbol" || e.ToLabel != "Symbol" {
			continue
		}
		if !semanticEdges[e.EdgeLabel] {
			continue
		}
		graph[e.FromKey] = append(graph[e.FromKey], e.ToKey)
		graph[e.ToKey] = append(graph[e.ToKey], e.FromKey)
	}

	communities := ranking.Louvain(graph)
	if communities == nil {
		// No edges — assign community 0 to all symbols.
		for i := range vertices {
			if vertices[i].Label == "Symbol" {
				vertices[i].Props["community"] = "0"
			}
		}
		return
	}

	for i := range vertices {
		if vertices[i].Label != "Symbol" {
			continue
		}
		key := vertices[i].Props["name"] + ":" + vertices[i].Props["file"]
		if cid, ok := communities[key]; ok {
			vertices[i].Props["community"] = strconv.Itoa(cid)
		}
	}
}
