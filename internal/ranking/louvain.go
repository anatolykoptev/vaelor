package ranking

const (
	maxCommunityFraction = 0.25
	minSplitSize         = 10
	maxLouvainPasses     = 50
)

// Louvain performs community detection on an undirected graph using the Louvain
// modularity optimization algorithm. Input is an adjacency list (same format as
// PageRank). Returns a map from node to community ID (0-indexed). Returns nil
// for empty input. Communities larger than 25% of the graph (min 10 nodes) are
// recursively split.
func Louvain(graph map[string][]string) map[string]int {
	if len(graph) == 0 {
		return nil
	}
	nodes, _ := collectNodes(graph)
	if len(nodes) == 0 {
		return nil
	}

	adj, sortedNodes := buildUndirectedAdj(graph)
	communities := coreLouvain(adj, sortedNodes)
	communities = splitOversized(communities, adj)
	return compactIDs(communities)
}

// coreLouvain runs the greedy modularity optimization loop (no oversized split).
// nodes must be sorted for deterministic output.
func coreLouvain(adj map[string][]string, nodes []string) map[string]int {
	if len(adj) == 0 {
		return map[string]int{}
	}

	degree := make(map[string]int, len(adj))
	twoM := 0
	for _, node := range nodes {
		degree[node] = len(adj[node])
		twoM += len(adj[node])
	}

	community := make(map[string]int, len(adj))
	for idx, node := range nodes {
		community[node] = idx
	}

	sigmaTot := make(map[int]int)
	for _, node := range nodes {
		sigmaTot[community[node]] += degree[node]
	}

	edgesToComm := make(map[int]int)

	for range maxLouvainPasses {
		improved := false

		for _, node := range nodes {
			currentComm := community[node]
			ki := degree[node]

			clear(edgesToComm)
			for _, nb := range adj[node] {
				edgesToComm[community[nb]]++
			}

			sigmaTotCurrent := sigmaTot[currentComm] - ki
			bestGain := float64(edgesToComm[currentComm]) - float64(ki)*float64(sigmaTotCurrent)/float64(twoM)
			bestComm := currentComm

			for comm, edgeCount := range edgesToComm {
				if comm == currentComm {
					continue
				}
				gain := float64(edgeCount) - float64(ki)*float64(sigmaTot[comm])/float64(twoM)
				if gain > bestGain {
					bestGain = gain
					bestComm = comm
				}
			}

			if bestComm != currentComm {
				sigmaTot[currentComm] -= ki
				community[node] = bestComm
				sigmaTot[bestComm] += ki
				improved = true
			}
		}

		if !improved {
			break
		}
	}

	return community
}
