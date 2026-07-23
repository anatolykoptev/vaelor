package ranking

import "context"

// LouvainWeighted performs community detection on a weighted undirected graph.
// Input: node → neighbor → edge weight. Uses γ = 1.0.
func LouvainWeighted(graph map[string]map[string]int) map[string]int {
	return LouvainWeightedCtx(context.Background(), graph)
}

// LouvainWeightedCtx is the context-aware variant. It checks ctx.Err() between
// Louvain levels and between passes; on ctx cancellation it returns nil (a
// partial/empty result) so a long community-detection run on a large call graph
// cannot blow past a soft deadline into a session-killing hard timeout (#534).
// With a never-canceled ctx (as LouvainWeighted passes) it produces the exact
// pre-ctx result.
func LouvainWeightedCtx(ctx context.Context, graph map[string]map[string]int) map[string]int {
	if len(graph) == 0 {
		return nil
	}

	adj, sortedNodes, weights := buildWeightedAdj(graph)
	if len(sortedNodes) == 0 {
		return nil
	}

	nodeToSuper := make(map[string]string, len(sortedNodes))
	for _, n := range sortedNodes {
		nodeToSuper[n] = n
	}

	currentAdj := adj
	currentNodes := sortedNodes
	currentWeights := weights

	for range maxLevels {
		if ctx.Err() != nil {
			return nil
		}
		communities := coreLouvainWeightedCtx(ctx, currentAdj, currentNodes, currentWeights, 1.0)
		if ctx.Err() != nil {
			return nil
		}

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
				newNodeToSuper[origNode] = commToName[communities[superNode]]
			}
			nodeToSuper = newNodeToSuper
		}

		superAdj, superNodes, superWeights := contractWeightedGraph(currentAdj, currentNodes, currentWeights, communities)
		if len(superNodes) >= len(currentNodes) {
			break
		}
		currentAdj = superAdj
		currentNodes = superNodes
		currentWeights = superWeights
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

// coreLouvainWeightedCtx runs Phase 1 with edge weights and resolution γ. It
// checks ctx.Err() between passes and between nodes so a canceled ctx bails
// promptly instead of completing all 50 passes (#534); with a never-canceled
// ctx (background) it runs the full Phase 1 unchanged.
func coreLouvainWeightedCtx(ctx context.Context, adj map[string][]string, nodes []string, weights map[[2]string]int, gamma float64) map[string]int {
	if len(adj) == 0 {
		return map[string]int{}
	}

	edgeW := func(a, b string) int {
		if a > b {
			a, b = b, a
		}
		return weights[[2]string{a, b}]
	}

	// Weighted degree: sum of edge weights for each node.
	degree := make(map[string]int, len(adj))
	twoM := 0
	for _, node := range nodes {
		for _, nb := range adj[node] {
			degree[node] += edgeW(node, nb)
		}
		twoM += degree[node]
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
		if ctx.Err() != nil {
			return community
		}
		improved := false

		for _, node := range nodes {
			currentComm := community[node]
			ki := degree[node]

			clear(edgesToComm)
			for _, nb := range adj[node] {
				edgesToComm[community[nb]] += edgeW(node, nb)
			}

			sigmaTotCurrent := sigmaTot[currentComm] - ki
			bestGain := float64(edgesToComm[currentComm]) - gamma*float64(ki)*float64(sigmaTotCurrent)/float64(twoM)
			bestComm := currentComm

			for comm, edgeCount := range edgesToComm {
				if comm == currentComm {
					continue
				}
				gain := float64(edgeCount) - gamma*float64(ki)*float64(sigmaTot[comm])/float64(twoM)
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
