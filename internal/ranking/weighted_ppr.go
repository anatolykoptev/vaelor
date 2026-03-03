package ranking

// inboundEdge pairs a source node index with its edge weight.
type inboundEdge struct {
	srcIdx int
	weight float64
}

// WeightedPersonalizedPageRank computes PageRank with personalization on a
// weighted directed graph. graph maps src→dst→weight. Seeds bias the
// personalization vector; iterations/damping control convergence.
func WeightedPersonalizedPageRank(
	graph map[string]map[string]float64,
	seeds map[string]float64,
	iterations int,
	damping float64,
) map[string]float64 {
	if len(graph) == 0 {
		return nil
	}

	nodes, nodeIndex := collectWeightedNodes(graph)
	n := len(nodes)
	if n == 0 {
		return nil
	}

	nf := float64(n)
	personalization := buildPersonalization(nodes, nodeIndex, seeds, nf)
	rank := uniformRanks(n, nf)

	inbound, totalOutWeight, danglingNodes := buildWeightedReverseGraph(graph, nodeIndex, n)

	rank = iterateWeightedPersonalized(
		rank, inbound, totalOutWeight, danglingNodes,
		personalization, iterations, damping,
	)

	return buildResultMap(nodes, rank)
}

// collectWeightedNodes extracts all unique nodes from a weighted adjacency map.
func collectWeightedNodes(graph map[string]map[string]float64) ([]string, map[string]int) {
	nodeSet := make(map[string]struct{})
	for src, targets := range graph {
		nodeSet[src] = struct{}{}
		for dst := range targets {
			nodeSet[dst] = struct{}{}
		}
	}

	nodeIndex := make(map[string]int, len(nodeSet))
	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodeIndex[node] = len(nodes)
		nodes = append(nodes, node)
	}
	return nodes, nodeIndex
}

// buildWeightedReverseGraph constructs inbound edge lists, total outgoing
// weight per node, and a list of dangling node indices.
func buildWeightedReverseGraph(
	graph map[string]map[string]float64,
	nodeIndex map[string]int,
	n int,
) ([][]inboundEdge, []float64, []int) {
	inbound := make([][]inboundEdge, n)
	totalOutWeight := make([]float64, n)

	for src, targets := range graph {
		srcIdx := nodeIndex[src]
		for dst, w := range targets {
			dstIdx := nodeIndex[dst]
			inbound[dstIdx] = append(inbound[dstIdx], inboundEdge{srcIdx: srcIdx, weight: w})
			totalOutWeight[srcIdx] += w
		}
	}

	var danglingNodes []int
	for i := range n {
		if totalOutWeight[i] == 0 {
			danglingNodes = append(danglingNodes, i)
		}
	}
	return inbound, totalOutWeight, danglingNodes
}

// iterateWeightedPersonalized runs personalized PageRank iterations where each
// edge's contribution is proportional to its weight / total outgoing weight.
func iterateWeightedPersonalized(
	rank []float64,
	inbound [][]inboundEdge,
	totalOutWeight []float64,
	danglingNodes []int,
	personalization []float64,
	iterations int,
	damping float64,
) []float64 {
	newRank := make([]float64, len(rank))

	for range iterations {
		danglingSum := 0.0
		for _, idx := range danglingNodes {
			danglingSum += rank[idx]
		}

		for i := range newRank {
			sum := 0.0
			for _, edge := range inbound[i] {
				if totalOutWeight[edge.srcIdx] > 0 {
					sum += rank[edge.srcIdx] * edge.weight / totalOutWeight[edge.srcIdx]
				}
			}

			teleport := (1 - damping) * personalization[i]
			danglingContrib := damping * danglingSum * personalization[i]
			newRank[i] = teleport + danglingContrib + damping*sum
		}

		rank, newRank = newRank, rank
	}

	return rank
}
