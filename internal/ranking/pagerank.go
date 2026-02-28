package ranking

// PageRank computes PageRank scores for nodes in a directed graph.
// graph maps each node to the list of nodes it links TO (outgoing edges).
// iterations controls convergence (20 is typical), damping is usually 0.85.
// Returns normalized scores (sum = 1.0).
func PageRank(graph map[string][]string, iterations int, damping float64) map[string]float64 {
	if len(graph) == 0 {
		return nil
	}

	// Step 1: Collect all unique nodes (sources AND targets).
	nodeSet := make(map[string]struct{})
	for src, targets := range graph {
		nodeSet[src] = struct{}{}
		for _, tgt := range targets {
			nodeSet[tgt] = struct{}{}
		}
	}

	n := len(nodeSet)
	if n == 0 {
		return nil
	}

	// Map nodes to indices for efficient iteration.
	nodeIndex := make(map[string]int, n)
	indexNode := make([]string, 0, n)
	for node := range nodeSet {
		nodeIndex[node] = len(indexNode)
		indexNode = append(indexNode, node)
	}

	// Step 2: Initialize ranks uniformly.
	nf := float64(n)
	rank := make([]float64, n)
	for i := range rank {
		rank[i] = 1.0 / nf
	}

	// Step 3: Build reverse graph (inbound links) and outgoing degree.
	// inbound[i] = list of node indices that link TO node i.
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

	// Identify dangling nodes (no outgoing edges) for rank redistribution.
	var danglingNodes []int
	for i := 0; i < n; i++ {
		if outDegree[i] == 0 {
			danglingNodes = append(danglingNodes, i)
		}
	}

	// Step 4: Iterate with dangling node redistribution.
	// Dangling nodes have no outgoing links, so their rank would be lost.
	// Standard fix: redistribute their rank uniformly to all nodes.
	base := (1 - damping) / nf
	newRank := make([]float64, n)

	for iter := 0; iter < iterations; iter++ {
		// Sum rank mass from all dangling nodes.
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

	// Step 5: Build result map.
	result := make(map[string]float64, n)
	for i, node := range indexNode {
		result[node] = rank[i]
	}

	return result
}
