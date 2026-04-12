package ranking

// LouvainWithResolution performs community detection with a resolution parameter γ.
// γ > 1.0 produces more (smaller) communities; γ < 1.0 produces fewer (larger) communities.
// γ = 1.0 is standard modularity (equivalent to Louvain()).
func LouvainWithResolution(graph map[string][]string, gamma float64) map[string]int {
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
		communities := coreLouvainGamma(currentAdj, currentNodes, gamma)

		distinct := countDistinct(communities)
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

		if distinct > 1 {
			newNodeToSuper := make(map[string]string, len(nodeToSuper))
			for origNode, superNode := range nodeToSuper {
				c := communities[superNode]
				newNodeToSuper[origNode] = commToName[c]
			}
			nodeToSuper = newNodeToSuper
		}

		superAdj, superNodes := contractGraph(currentAdj, currentNodes, communities)

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
