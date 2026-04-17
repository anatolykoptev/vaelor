package codegraph

import (
	"strconv"

	"github.com/anatolykoptev/go-code/internal/ranking"
)

// edgeWeights defines semantic importance for codegraph edge types.
var edgeWeights = map[string]int{
	"CALLS":      3,
	"IMPLEMENTS": 2,
	"INHERITS":   2,
	"TESTED_BY":  1,
	"USES":       1,
}

// injectCommunities runs weighted Louvain community detection on Symbol-level edges
// and sets the "community" property on each Symbol vertex in-place.
// Must be called after buildGraph and before insertBatches.
func injectCommunities(vertices []vertexData, edges []edgeData) {
	graph := make(map[string]map[string]int)

	// Ensure all symbols are nodes (even if isolated).
	for _, v := range vertices {
		if v.Label == "Symbol" {
			key := v.Props["name"] + ":" + v.Props["file"]
			if graph[key] == nil {
				graph[key] = make(map[string]int)
			}
		}
	}

	for _, e := range edges {
		if e.FromLabel != "Symbol" || e.ToLabel != "Symbol" {
			continue
		}
		w, ok := edgeWeights[e.EdgeLabel]
		if !ok {
			continue
		}
		if graph[e.FromKey] == nil {
			graph[e.FromKey] = make(map[string]int)
		}
		if graph[e.ToKey] == nil {
			graph[e.ToKey] = make(map[string]int)
		}
		// Use max weight if multiple edges exist between same pair.
		if cur := graph[e.FromKey][e.ToKey]; w > cur {
			graph[e.FromKey][e.ToKey] = w
		}
		if cur := graph[e.ToKey][e.FromKey]; w > cur {
			graph[e.ToKey][e.FromKey] = w
		}
	}

	communities := ranking.LouvainWeighted(graph)
	if communities == nil {
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
