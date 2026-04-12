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

	adj := buildUndirectedAdj(graph)
	communities := coreLouvain(adj)
	communities = splitOversized(communities, adj)
	return compactIDs(communities)
}

// coreLouvain runs the greedy modularity optimization loop (no oversized split).
// Returns a map from node to community ID (not necessarily compact).
func coreLouvain(adj map[string][]string) map[string]int {
	if len(adj) == 0 {
		return map[string]int{}
	}

	// Compute degrees and total edge count (2m = sum of all degrees)
	degree := make(map[string]int, len(adj))
	twoM := 0
	for node, neighbors := range adj {
		degree[node] = len(neighbors)
		twoM += len(neighbors)
	}
	// twoM is already 2*edges because each edge appears twice in undirected adj

	// Initialize each node in its own community
	community := make(map[string]int, len(adj))
	idx := 0
	for node := range adj {
		community[node] = idx
		idx++
	}

	// Build sigma_tot: sum of degrees in each community
	sigmaTot := make(map[int]int)
	for node, comm := range community {
		sigmaTot[comm] += degree[node]
	}

	// Greedy optimization passes
	for pass := 0; pass < maxLouvainPasses; pass++ {
		improved := false

		for node, neighbors := range adj {
			currentComm := community[node]
			ki := degree[node]

			// Count edges to each neighboring community
			edgesToComm := make(map[int]int)
			for _, nb := range neighbors {
				edgesToComm[community[nb]]++
			}

			// Compute gain for current community (as baseline)
			// gain = edges_to_community - (ki * sigma_tot) / (2m)
			// We remove node from its community first (conceptually)
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
				// Move node to bestComm
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
