package codegraph

import (
	"fmt"
	"sort"
)

// surpriseEdge holds the data needed to score one edge's surprise factor.
type surpriseEdge struct {
	FromName  string
	FromFile  string
	FromPkg   string
	ToName    string
	ToFile    string
	ToPkg     string
	EdgeLabel string

	FromCommunity int
	ToCommunity   int
	FromDegree    int
	ToDegree      int
	FromPageRank  float64
	ToPageRank    float64
}

// SurpriseResult is a scored surprise edge with human-readable reasons.
type SurpriseResult struct {
	FromName string   `json:"from_name"`
	FromFile string   `json:"from_file"`
	ToName   string   `json:"to_name"`
	ToFile   string   `json:"to_file"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
}

// scoreSurprise computes a surprise score and reasons for a single edge.
func scoreSurprise(e surpriseEdge) (int, []string) {
	score := 0
	var reasons []string

	// 1. Cross-package: symbols in different packages are non-trivially coupled.
	if e.FromPkg != "" && e.ToPkg != "" && e.FromPkg != e.ToPkg {
		score += 2
		reasons = append(reasons, fmt.Sprintf("crosses package boundary (%s → %s)", e.FromPkg, e.ToPkg))
	}

	// 2. Cross-community: Louvain says these are structurally distant.
	if e.FromCommunity != e.ToCommunity {
		score += 1
		reasons = append(reasons, "bridges separate communities")
	}

	// 3. Peripheral→hub: low-degree node reaches a high-degree one.
	minDeg := min(e.FromDegree, e.ToDegree)
	maxDeg := max(e.FromDegree, e.ToDegree)
	if minDeg <= 2 && maxDeg >= 5 {
		score += 1
		peripheral := e.FromName
		hub := e.ToName
		if e.FromDegree > e.ToDegree {
			peripheral, hub = hub, peripheral
		}
		reasons = append(reasons, fmt.Sprintf("peripheral `%s` reaches hub `%s`", peripheral, hub))
	}

	// 4. PageRank gap: >10x difference suggests an unexpected dependency path.
	if e.FromPageRank > 0 && e.ToPageRank > 0 {
		ratio := e.ToPageRank / e.FromPageRank
		if ratio < 1 {
			ratio = 1 / ratio
		}
		if ratio > 10 {
			score += 1
			reasons = append(reasons, "large PageRank gap (>10x)")
		}
	}

	// 5. Cross-file bonus (different source files).
	if e.FromFile != e.ToFile {
		score += 1
		reasons = append(reasons, "cross-file dependency")
	}

	return score, reasons
}

// rankSurprises scores all edges and returns top-N sorted by score descending.
func rankSurprises(edges []surpriseEdge, topN int) []SurpriseResult {
	var results []SurpriseResult
	for _, e := range edges {
		score, reasons := scoreSurprise(e)
		if score == 0 {
			continue
		}
		results = append(results, SurpriseResult{
			FromName: e.FromName,
			FromFile: e.FromFile,
			ToName:   e.ToName,
			ToFile:   e.ToFile,
			Score:    score,
			Reasons:  reasons,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topN {
		results = results[:topN]
	}
	return results
}
