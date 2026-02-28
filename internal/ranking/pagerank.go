package ranking

// PageRank computes PageRank scores for nodes in a directed graph.
// graph maps each node to the list of nodes it links TO (outgoing edges).
// iterations controls convergence (20 is typical), damping is usually 0.85.
// Returns normalized scores (sum ≈ 1.0).
func PageRank(graph map[string][]string, iterations int, damping float64) map[string]float64 {
	if len(graph) == 0 {
		return nil
	}

	nodes, nodeIndex := collectNodes(graph)
	n := len(nodes)
	if n == 0 {
		return nil
	}

	nf := float64(n)
	rank := uniformRanks(n, nf)

	inbound, outDegree := buildReverseGraph(graph, nodeIndex, n)
	danglingNodes := findDanglingNodes(outDegree, n)

	rank = iterate(rank, inbound, outDegree, danglingNodes, iterations, damping, nf)

	return buildResultMap(nodes, rank)
}

// collectNodes extracts all unique nodes and assigns indices.
func collectNodes(graph map[string][]string) ([]string, map[string]int) {
	nodeSet := make(map[string]struct{})
	for src, targets := range graph {
		nodeSet[src] = struct{}{}
		for _, tgt := range targets {
			nodeSet[tgt] = struct{}{}
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

// uniformRanks initializes a rank slice with uniform values.
func uniformRanks(n int, nf float64) []float64 {
	rank := make([]float64, n)
	for i := range rank {
		rank[i] = 1.0 / nf
	}
	return rank
}

// buildReverseGraph constructs inbound links and outgoing degree arrays.
func buildReverseGraph(graph map[string][]string, nodeIndex map[string]int, n int) ([][]int, []int) {
	inbound := make([][]int, n)
	outDegree := make([]int, n)

	for src, targets := range graph {
		srcIdx := nodeIndex[src]
		outDegree[srcIdx] = len(targets)
		for _, tgt := range targets {
			tgtIdx := nodeIndex[tgt]
			inbound[tgtIdx] = append(inbound[tgtIdx], srcIdx)
		}
	}
	return inbound, outDegree
}

// findDanglingNodes returns indices of nodes with no outgoing edges.
func findDanglingNodes(outDegree []int, n int) []int {
	var dangling []int
	for i := range n {
		if outDegree[i] == 0 {
			dangling = append(dangling, i)
		}
	}
	return dangling
}

// iterate runs the PageRank iterations with dangling node redistribution.
func iterate(rank []float64, inbound [][]int, outDegree []int, danglingNodes []int, iterations int, damping, nf float64) []float64 {
	base := (1 - damping) / nf
	newRank := make([]float64, len(rank))

	for range iterations {
		danglingSum := 0.0
		for _, idx := range danglingNodes {
			danglingSum += rank[idx]
		}
		danglingContrib := damping * danglingSum / nf

		for i := range newRank {
			sum := 0.0
			for _, srcIdx := range inbound[i] {
				if outDegree[srcIdx] > 0 {
					sum += rank[srcIdx] / float64(outDegree[srcIdx])
				}
			}
			newRank[i] = base + danglingContrib + damping*sum
		}
		rank, newRank = newRank, rank
	}
	return rank
}

// buildResultMap converts indexed ranks back to a string-keyed map.
func buildResultMap(nodes []string, rank []float64) map[string]float64 {
	result := make(map[string]float64, len(nodes))
	for i, node := range nodes {
		result[node] = rank[i]
	}
	return result
}
