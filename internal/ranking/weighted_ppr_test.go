package ranking

import (
	"math"
	"testing"
)

func TestWeightedPPR_HighWeightEdge(t *testing.T) {
	// hub.go distributes rank to heavy.go (weight 5) and light.go (weight 1).
	// heavy.go should receive more rank because it gets a larger share of hub's outflow.
	graph := map[string]map[string]float64{
		"hub.go":   {"heavy.go": 5.0, "light.go": 1.0},
		"heavy.go": {},
		"light.go": {},
	}
	result := WeightedPersonalizedPageRank(graph, nil, 20, 0.85)
	if result["heavy.go"] <= result["light.go"] {
		t.Errorf("heavy.go (%f) should outrank light.go (%f) due to stronger inbound edge",
			result["heavy.go"], result["light.go"])
	}
}

func TestWeightedPPR_SeedBias(t *testing.T) {
	graph := map[string]map[string]float64{
		"a.go":    {"core.go": 1.0},
		"b.go":    {"core.go": 1.0},
		"c.go":    {"core.go": 1.0},
		"core.go": {},
	}
	seeds := map[string]float64{"a.go": 1.0}
	result := WeightedPersonalizedPageRank(graph, seeds, 20, 0.85)
	if result["a.go"] <= result["b.go"] {
		t.Errorf("seed a.go (%f) should outrank non-seed b.go (%f)", result["a.go"], result["b.go"])
	}
}

func TestWeightedPPR_EmptyGraph(t *testing.T) {
	if WeightedPersonalizedPageRank(nil, nil, 20, 0.85) != nil {
		t.Error("expected nil for nil graph")
	}
	empty := map[string]map[string]float64{}
	if WeightedPersonalizedPageRank(empty, nil, 20, 0.85) != nil {
		t.Error("expected nil for empty graph")
	}
}

func TestWeightedPPR_UniformWeights_MatchesUnweighted(t *testing.T) {
	unweighted := map[string][]string{
		"a.go": {"b.go"}, "b.go": {"c.go"}, "c.go": {"a.go"},
	}
	weighted := map[string]map[string]float64{
		"a.go": {"b.go": 1.0}, "b.go": {"c.go": 1.0}, "c.go": {"a.go": 1.0},
	}
	seeds := map[string]float64{"a.go": 1.0}

	ppr := PersonalizedPageRank(unweighted, seeds, 20, 0.85)
	wppr := WeightedPersonalizedPageRank(weighted, seeds, 20, 0.85)

	for node, score := range ppr {
		if math.Abs(score-wppr[node]) > 0.01 {
			t.Errorf("node %s: unweighted=%f weighted=%f — should match with uniform weights",
				node, score, wppr[node])
		}
	}
}

func TestWeightedPPR_Normalized(t *testing.T) {
	graph := map[string]map[string]float64{
		"a.go": {"b.go": 2.0, "c.go": 0.5},
		"b.go": {"c.go": 1.0},
		"c.go": {},
	}
	seeds := map[string]float64{"a.go": 1.0}
	result := WeightedPersonalizedPageRank(graph, seeds, 20, 0.85)
	sum := 0.0
	for _, rank := range result {
		sum += rank
	}
	if math.Abs(sum-1.0) > 0.05 {
		t.Errorf("rank sum = %f, expected ~1.0", sum)
	}
}
