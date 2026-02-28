package ranking

import (
	"math"
	"testing"
)

func TestPageRank_EmptyGraph(t *testing.T) {
	result := PageRank(nil, 20, 0.85)
	if result != nil {
		t.Errorf("expected nil for empty graph, got %v", result)
	}

	result = PageRank(map[string][]string{}, 20, 0.85)
	if result != nil {
		t.Errorf("expected nil for empty map, got %v", result)
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	graph := map[string][]string{
		"core.go": {},
	}
	result := PageRank(graph, 20, 0.85)

	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if _, ok := result["core.go"]; !ok {
		t.Fatal("expected core.go in result")
	}
	// Single node gets rank 1/N = 1.0 initially, converges to (1-d)/N + 0 = 0.15 for no inbound.
	// But with no outgoing either, it keeps getting (1-d)/N = 0.15 each iteration.
	if result["core.go"] <= 0 {
		t.Errorf("expected positive rank, got %f", result["core.go"])
	}
}

func TestPageRank_StarTopology(t *testing.T) {
	// All files import "core.go" → core.go should have the highest rank.
	graph := map[string][]string{
		"a.go":    {"core.go"},
		"b.go":    {"core.go"},
		"c.go":    {"core.go"},
		"d.go":    {"core.go"},
		"core.go": {},
	}
	result := PageRank(graph, 20, 0.85)

	if len(result) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(result))
	}

	coreRank := result["core.go"]
	for name, rank := range result {
		if name != "core.go" && rank >= coreRank {
			t.Errorf("expected core.go to have highest rank, but %s (%f) >= core.go (%f)",
				name, rank, coreRank)
		}
	}
}

func TestPageRank_ChainTopology(t *testing.T) {
	// a→b→c: importance flows downstream, so c > b > a.
	graph := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {},
	}
	result := PageRank(graph, 20, 0.85)

	if len(result) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result))
	}

	if result["c.go"] <= result["b.go"] {
		t.Errorf("expected c > b, got c=%f, b=%f", result["c.go"], result["b.go"])
	}
	if result["b.go"] <= result["a.go"] {
		t.Errorf("expected b > a, got b=%f, a=%f", result["b.go"], result["a.go"])
	}
}

func TestPageRank_Convergence(t *testing.T) {
	// Cycle: a→b→c→a — all nodes should converge to roughly equal ranks.
	graph := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {"a.go"},
	}
	result := PageRank(graph, 20, 0.85)

	if len(result) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result))
	}

	expected := 1.0 / 3.0
	const tolerance = 0.01

	for name, rank := range result {
		if math.Abs(rank-expected) > tolerance {
			t.Errorf("node %s: expected ~%f, got %f (tolerance %f)",
				name, expected, rank, tolerance)
		}
	}
}

func TestPageRank_Normalized(t *testing.T) {
	// Various graph shapes — ranks should sum to approximately 1.0.
	graphs := []map[string][]string{
		// Star topology.
		{
			"a.go":    {"core.go"},
			"b.go":    {"core.go"},
			"core.go": {},
		},
		// Chain topology.
		{
			"a.go": {"b.go"},
			"b.go": {"c.go"},
			"c.go": {"d.go"},
			"d.go": {},
		},
		// Cycle with spur.
		{
			"a.go": {"b.go"},
			"b.go": {"c.go"},
			"c.go": {"a.go"},
			"d.go": {"a.go"},
		},
	}

	const tolerance = 0.05

	for i, graph := range graphs {
		result := PageRank(graph, 20, 0.85)
		sum := 0.0
		for _, rank := range result {
			sum += rank
		}
		if math.Abs(sum-1.0) > tolerance {
			t.Errorf("graph %d: rank sum = %f, expected ~1.0 (tolerance %f)", i, sum, tolerance)
		}
	}
}
