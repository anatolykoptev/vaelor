package ranking

// PersonalizedPageRank computes PageRank with a personalization vector.
// Seeds maps node names to personalization weights (higher = more bias).
// When seeds is nil, falls back to uniform (standard PageRank).
func PersonalizedPageRank(
	graph map[string][]string,
	seeds map[string]float64,
	iterations int,
	damping float64,
) map[string]float64 {
	if len(graph) == 0 {
		return nil
	}

	nodes, nodeIndex := collectNodes(graph)
	n := len(nodes)
	if n == 0 {
		return nil
	}

	nf := float64(n)
	personalization := buildPersonalization(nodes, nodeIndex, seeds, nf)
	rank := uniformRanks(n, nf)

	inbound, outDegree := buildReverseGraph(graph, nodeIndex, n)
	danglingNodes := findDanglingNodes(outDegree, n)

	rank = iteratePersonalized(
		rank, inbound, outDegree, danglingNodes,
		personalization, iterations, damping,
	)

	return buildResultMap(nodes, rank)
}

// buildPersonalization creates a normalized personalization vector.
// With no seeds, returns uniform distribution (equivalent to standard PageRank).
func buildPersonalization(
	nodes []string,
	nodeIndex map[string]int,
	seeds map[string]float64,
	nf float64,
) []float64 {
	n := len(nodes)
	p := make([]float64, n)

	if len(seeds) == 0 {
		for i := range p {
			p[i] = 1.0 / nf
		}
		return p
	}

	totalWeight := 0.0
	for _, w := range seeds {
		totalWeight += w
	}

	for node, w := range seeds {
		if idx, ok := nodeIndex[node]; ok {
			p[idx] = w / totalWeight
		}
	}

	return p
}

// iteratePersonalized runs PageRank iterations using a personalization vector
// instead of uniform teleportation.
func iteratePersonalized(
	rank []float64,
	inbound [][]int,
	outDegree []int,
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
			for _, srcIdx := range inbound[i] {
				if outDegree[srcIdx] > 0 {
					sum += rank[srcIdx] / float64(outDegree[srcIdx])
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
