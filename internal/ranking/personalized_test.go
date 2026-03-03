package ranking

import (
	"math"
	"testing"
)

func TestPPR_SeedBias(t *testing.T) {
	graph := map[string][]string{
		"a.go": {"core.go"}, "b.go": {"core.go"},
		"c.go": {"core.go"}, "core.go": {},
	}
	seeds := map[string]float64{"a.go": 1.0}
	result := PersonalizedPageRank(graph, seeds, 20, 0.85)
	if result["a.go"] <= result["b.go"] {
		t.Errorf("seed a.go (%f) should outrank non-seed b.go (%f)", result["a.go"], result["b.go"])
	}
}

func TestPPR_NoSeeds_EqualsStandard(t *testing.T) {
	graph := map[string][]string{"a.go": {"b.go"}, "b.go": {"c.go"}, "c.go": {"a.go"}}
	ppr := PersonalizedPageRank(graph, nil, 20, 0.85)
	pr := PageRank(graph, 20, 0.85)
	for node := range pr {
		if math.Abs(ppr[node]-pr[node]) > 0.01 {
			t.Errorf("node %s: PPR=%f PR=%f — should be equal with no seeds", node, ppr[node], pr[node])
		}
	}
}

func TestPPR_EmptyGraph(t *testing.T) {
	if PersonalizedPageRank(nil, nil, 20, 0.85) != nil {
		t.Error("expected nil for empty graph")
	}
}

func TestPPR_MultipleSeeds(t *testing.T) {
	graph := map[string][]string{"a.go": {"core.go"}, "b.go": {"core.go"}, "c.go": {}, "core.go": {}}
	seeds := map[string]float64{"a.go": 1.0, "b.go": 1.0}
	result := PersonalizedPageRank(graph, seeds, 20, 0.85)
	if result["a.go"] <= result["c.go"] {
		t.Errorf("seed a.go should outrank non-seed c.go")
	}
}

func TestPPR_Normalized(t *testing.T) {
	graph := map[string][]string{"a.go": {"b.go"}, "b.go": {"c.go"}, "c.go": {}}
	seeds := map[string]float64{"a.go": 1.0}
	result := PersonalizedPageRank(graph, seeds, 20, 0.85)
	sum := 0.0
	for _, rank := range result {
		sum += rank
	}
	if math.Abs(sum-1.0) > 0.05 {
		t.Errorf("rank sum = %f, expected ~1.0", sum)
	}
}
