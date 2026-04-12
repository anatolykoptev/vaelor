package ranking

import (
	"slices"
)

// splitOversized finds communities larger than maxCommunityFraction of the total
// graph (and >= minSplitSize nodes) and recursively splits them via a second
// Louvain pass on the subgraph.
func splitOversized(community map[string]int, adj map[string][]string) map[string]int {
	total := len(community)
	threshold := int(float64(total) * maxCommunityFraction)
	if threshold < minSplitSize {
		threshold = minSplitSize
	}

	groups := make(map[int][]string)
	for node, comm := range community {
		groups[comm] = append(groups[comm], node)
	}

	maxID := 0
	for _, comm := range community {
		if comm > maxID {
			maxID = comm
		}
	}

	result := make(map[string]int, len(community))
	for node, comm := range community {
		result[node] = comm
	}

	for comm, members := range groups {
		if len(members) < threshold {
			continue
		}
		memberSet := make(map[string]struct{}, len(members))
		for _, m := range members {
			memberSet[m] = struct{}{}
		}
		subAdj := make(map[string][]string, len(members))
		for _, m := range members {
			subAdj[m] = []string{}
			for _, nb := range adj[m] {
				if _, inSet := memberSet[nb]; inSet {
					subAdj[m] = append(subAdj[m], nb)
				}
			}
		}

		subNodes := make([]string, len(members))
		copy(subNodes, members)
		slices.Sort(subNodes)

		subCommunity := coreLouvain(subAdj, subNodes)
		subCommunity = compactIDs(subCommunity)

		if countDistinct(subCommunity) <= 1 {
			continue
		}

		subIDMap := make(map[int]int)
		nextID := maxID + 1
		for _, subID := range subCommunity {
			if _, mapped := subIDMap[subID]; !mapped {
				if subID == 0 {
					subIDMap[subID] = comm
				} else {
					subIDMap[subID] = nextID
					nextID++
				}
			}
		}
		maxID = nextID - 1

		for _, m := range members {
			result[m] = subIDMap[subCommunity[m]]
		}
	}

	return result
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
