package ranking

import (
	"slices"
)

// contractGraph merges nodes by community assignment, producing a super-graph.
// Returns adjacency map and sorted super-node names.
func contractGraph(
	adj map[string][]string,
	nodes []string,
	community map[string]int,
) (superAdj map[string][]string, superNodes []string) {
	commName := make(map[int]string)
	for _, node := range nodes {
		c := community[node]
		if _, ok := commName[c]; !ok {
			commName[c] = node
		}
	}

	// Find edges between communities.
	type edgeKey = [2]string
	edgeExists := make(map[edgeKey]bool)

	for _, node := range nodes {
		cName := commName[community[node]]
		for _, nb := range adj[node] {
			nbName := commName[community[nb]]
			if cName == nbName {
				continue
			}
			a, b := cName, nbName
			if a > b {
				a, b = b, a
			}
			edgeExists[edgeKey{a, b}] = true
		}
	}

	// Build super-adjacency.
	superSet := make(map[string]map[string]bool)
	for ek := range edgeExists {
		if superSet[ek[0]] == nil {
			superSet[ek[0]] = make(map[string]bool)
		}
		if superSet[ek[1]] == nil {
			superSet[ek[1]] = make(map[string]bool)
		}
		superSet[ek[0]][ek[1]] = true
		superSet[ek[1]][ek[0]] = true
	}

	// Ensure isolated super-nodes exist.
	for _, name := range commName {
		if _, ok := superSet[name]; !ok {
			superSet[name] = make(map[string]bool)
		}
	}

	superAdj = make(map[string][]string, len(superSet))
	superNodes = make([]string, 0, len(superSet))
	for name, nbs := range superSet {
		superNodes = append(superNodes, name)
		nbList := make([]string, 0, len(nbs))
		for nb := range nbs {
			nbList = append(nbList, nb)
		}
		slices.Sort(nbList)
		superAdj[name] = nbList
	}
	slices.Sort(superNodes)

	return superAdj, superNodes
}

// buildUndirectedAdj builds a symmetric adjacency list from a directed input graph,
// deduplicating edges and skipping self-loops. Returns the adjacency map and a
// sorted node list for deterministic iteration.
func buildUndirectedAdj(graph map[string][]string) (map[string][]string, []string) {
	nodes, _ := collectNodes(graph)
	slices.Sort(nodes)

	adj := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		adj[n] = []string{}
	}

	type edge struct{ a, b string }
	seen := make(map[edge]struct{})

	addEdge := func(a, b string) {
		if a == b {
			return
		}
		if a > b {
			a, b = b, a
		}
		e := edge{a, b}
		if _, exists := seen[e]; exists {
			return
		}
		seen[e] = struct{}{}
		adj[e.a] = append(adj[e.a], e.b)
		adj[e.b] = append(adj[e.b], e.a)
	}

	for src, targets := range graph {
		for _, tgt := range targets {
			addEdge(src, tgt)
		}
	}

	for _, neighbors := range adj {
		slices.Sort(neighbors)
	}

	return adj, nodes
}

// compactIDs remaps community IDs to a contiguous range 0..k-1.
// Iterates nodes in sorted order for deterministic ID assignment.
func compactIDs(community map[string]int) map[string]int {
	nodes := make([]string, 0, len(community))
	for node := range community {
		nodes = append(nodes, node)
	}
	slices.Sort(nodes)

	remap := make(map[int]int)
	next := 0
	result := make(map[string]int, len(community))
	for _, node := range nodes {
		comm := community[node]
		if _, ok := remap[comm]; !ok {
			remap[comm] = next
			next++
		}
		result[node] = remap[comm]
	}
	return result
}

// countDistinct returns the number of unique values in a map[string]int.
func countDistinct(m map[string]int) int {
	seen := make(map[int]struct{})
	for _, v := range m {
		seen[v] = struct{}{}
	}
	return len(seen)
}
