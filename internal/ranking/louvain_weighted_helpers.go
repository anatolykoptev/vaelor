package ranking

import "slices"

// buildWeightedAdj builds a symmetric weighted adjacency from a directed weighted input.
// Deduplicates edges (takes max weight) and skips self-loops.
func buildWeightedAdj(graph map[string]map[string]int) (adj map[string][]string, nodes []string, weights map[[2]string]int) {
	nodeSet := make(map[string]struct{})
	for src, targets := range graph {
		nodeSet[src] = struct{}{}
		for tgt := range targets {
			nodeSet[tgt] = struct{}{}
		}
	}

	nodes = make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	slices.Sort(nodes)

	adj = make(map[string][]string, len(nodes))
	for _, n := range nodes {
		adj[n] = []string{}
	}

	weights = make(map[[2]string]int)
	seen := make(map[[2]string]bool)

	for src, targets := range graph {
		for tgt, w := range targets {
			if src == tgt {
				continue
			}
			a, b := src, tgt
			if a > b {
				a, b = b, a
			}
			key := [2]string{a, b}
			if w > weights[key] {
				weights[key] = w
			}
			if !seen[key] {
				seen[key] = true
				adj[a] = append(adj[a], b)
				adj[b] = append(adj[b], a)
			}
		}
	}

	for _, nbs := range adj {
		slices.Sort(nbs)
	}

	return adj, nodes, weights
}

// contractWeightedGraph merges communities into super-nodes, summing edge weights.
func contractWeightedGraph(
	adj map[string][]string,
	nodes []string,
	weights map[[2]string]int,
	community map[string]int,
) (superAdj map[string][]string, superNodes []string, superWeights map[[2]string]int) {
	edgeW := func(a, b string) int {
		if a > b {
			a, b = b, a
		}
		return weights[[2]string{a, b}]
	}

	commName := make(map[int]string)
	for _, node := range nodes {
		c := community[node]
		if _, ok := commName[c]; !ok {
			commName[c] = node
		}
	}

	superWeights = make(map[[2]string]int)
	for _, node := range nodes {
		cName := commName[community[node]]
		for _, nb := range adj[node] {
			nbName := commName[community[nb]]
			if cName == nbName {
				continue
			}
			a, b := cName, nbName
			if a > b {
				a, b = b, a
			}
			superWeights[[2]string{a, b}] += edgeW(node, nb)
		}
	}

	// Halve weights (each edge counted from both sides).
	for k, v := range superWeights {
		superWeights[k] = v / 2
	}

	superSet := make(map[string]map[string]bool)
	for ek := range superWeights {
		if superSet[ek[0]] == nil {
			superSet[ek[0]] = make(map[string]bool)
		}
		if superSet[ek[1]] == nil {
			superSet[ek[1]] = make(map[string]bool)
		}
		superSet[ek[0]][ek[1]] = true
		superSet[ek[1]][ek[0]] = true
	}
	for _, name := range commName {
		if _, ok := superSet[name]; !ok {
			superSet[name] = make(map[string]bool)
		}
	}

	superAdj = make(map[string][]string, len(superSet))
	superNodes = make([]string, 0, len(superSet))
	for name, nbs := range superSet {
		superNodes = append(superNodes, name)
		nbList := make([]string, 0, len(nbs))
		for nb := range nbs {
			nbList = append(nbList, nb)
		}
		slices.Sort(nbList)
		superAdj[name] = nbList
	}
	slices.Sort(superNodes)

	return superAdj, superNodes, superWeights
}
