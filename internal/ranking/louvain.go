package ranking

const (
	maxLouvainPasses = 50
	maxLevels        = 10
)

// Louvain performs community detection on an undirected graph using the Louvain
// modularity optimization algorithm. Input is an adjacency list (same format as
// PageRank). Returns a map from node to community ID (0-indexed). Returns nil
// for empty input. Uses multi-level graph contraction for better cluster separation.
func Louvain(graph map[string][]string) map[string]int {
	if len(graph) == 0 {
		return nil
	}
	nodes, _ := collectNodes(graph)
	if len(nodes) == 0 {
		return nil
	}

	adj, sortedNodes := buildUndirectedAdj(graph)

	nodeToSuper := make(map[string]string, len(sortedNodes))
	for _, n := range sortedNodes {
		nodeToSuper[n] = n
	}

	currentAdj := adj
	currentNodes := sortedNodes

	for range maxLevels {
		communities := coreLouvain(currentAdj, currentNodes)

		distinct := countDistinct(communities)
		// No further improvement possible.
		if distinct >= len(currentNodes) {
			break
		}

		commToName := make(map[int]string)
		for _, node := range currentNodes {
			c := communities[node]
			if _, ok := commToName[c]; !ok {
				commToName[c] = node
			}
		}

		// Always propagate community assignments to original nodes when we have
		// a non-trivial partition (distinct > 1).
		if distinct > 1 {
			newNodeToSuper := make(map[string]string, len(nodeToSuper))
			for origNode, superNode := range nodeToSuper {
				c := communities[superNode]
				newNodeToSuper[origNode] = commToName[c]
			}
			nodeToSuper = newNodeToSuper
		}

		superAdj, superNodes := contractGraph(currentAdj, currentNodes, communities)

		// Stop if contraction didn't reduce graph size (or everything collapsed to 1).
		if len(superNodes) >= len(currentNodes) {
			break
		}

		currentAdj = superAdj
		currentNodes = superNodes
	}

	superToID := make(map[string]int)
	nextID := 0
	result := make(map[string]int, len(nodeToSuper))
	for _, origNode := range sortedNodes {
		superNode := nodeToSuper[origNode]
		if _, ok := superToID[superNode]; !ok {
			superToID[superNode] = nextID
			nextID++
		}
		result[origNode] = superToID[superNode]
	}

	return result
}

// coreLouvain runs the greedy modularity optimization loop.
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
