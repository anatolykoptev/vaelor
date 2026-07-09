package ranking

import (
	"testing"
)

// TestLouvain_TwoClusters verifies two triangles connected by a bridge edge
// land in the same community within each triangle but different communities overall.
func TestLouvain_TwoClusters(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"a": {"b", "c"},
		"b": {"a", "c"},
		"c": {"a", "b", "d"},
		"d": {"c", "e", "f"},
		"e": {"d", "f"},
		"f": {"d", "e"},
	}
	communities := Louvain(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	for _, node := range []string{"a", "b", "c", "d", "e", "f"} {
		if _, ok := communities[node]; !ok {
			t.Errorf("node %q missing from result", node)
		}
	}
	// a, b, c should be in the same community
	if communities["a"] != communities["b"] || communities["a"] != communities["c"] {
		t.Errorf("expected a, b, c in same community, got %v", communities)
	}
	// d, e, f should be in the same community
	if communities["d"] != communities["e"] || communities["d"] != communities["f"] {
		t.Errorf("expected d, e, f in same community, got %v", communities)
	}
	// the two groups should be in different communities
	if communities["a"] == communities["d"] {
		t.Errorf("expected two-cluster separation, got same community for a and d: %v", communities)
	}
}

// TestLouvain_EmptyGraph verifies nil input returns nil output.
func TestLouvain_EmptyGraph(t *testing.T) {
	t.Parallel()
	result := Louvain(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

// TestLouvain_SingleNode verifies a single node with no edges gets a community assigned.
func TestLouvain_SingleNode(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"a": {},
	}
	communities := Louvain(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	if _, ok := communities["a"]; !ok {
		t.Error("expected community assigned to node 'a'")
	}
}

// TestLouvain_DisconnectedComponents verifies two isolated pairs land in different communities.
func TestLouvain_DisconnectedComponents(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"a": {"b"},
		"b": {"a"},
		"c": {"d"},
		"d": {"c"},
	}
	communities := Louvain(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	for _, node := range []string{"a", "b", "c", "d"} {
		if _, ok := communities[node]; !ok {
			t.Errorf("node %q missing from result", node)
		}
	}
	// a and b should share a community
	if communities["a"] != communities["b"] {
		t.Errorf("expected a and b in same community, got %v", communities)
	}
	// c and d should share a community
	if communities["c"] != communities["d"] {
		t.Errorf("expected c and d in same community, got %v", communities)
	}
	// the two pairs should be in different communities
	if communities["a"] == communities["c"] {
		t.Errorf("expected disconnected components in different communities, got %v", communities)
	}
}

// TestLouvain_Deterministic verifies identical input produces identical output across 10 runs.
func TestLouvain_Deterministic(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"a": {"b", "c"},
		"b": {"a", "c"},
		"c": {"a", "b", "d"},
		"d": {"c", "e", "f"},
		"e": {"d", "f"},
		"f": {"d", "e"},
	}
	baseline := Louvain(graph)
	for i := range 10 {
		result := Louvain(graph)
		for node, comm := range baseline {
			if result[node] != comm {
				t.Fatalf("run %d: node %q got community %d, want %d", i, node, result[node], comm)
			}
		}
	}
}

// TestLouvain_ThreeClusters verifies three well-separated clusters are detected.
func TestLouvain_ThreeClusters(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"a1": {"a2", "a3"},
		"a2": {"a1", "a3"},
		"a3": {"a1", "a2", "b1"},
		"b1": {"a3", "b2", "b3"},
		"b2": {"b1", "b3"},
		"b3": {"b1", "b2", "c1"},
		"c1": {"b3", "c2", "c3"},
		"c2": {"c1", "c3"},
		"c3": {"c1", "c2"},
	}
	communities := Louvain(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	if communities["a1"] != communities["a2"] || communities["a1"] != communities["a3"] {
		t.Errorf("a-cluster split: %v", communities)
	}
	if communities["b1"] != communities["b2"] || communities["b1"] != communities["b3"] {
		t.Errorf("b-cluster split: %v", communities)
	}
	if communities["c1"] != communities["c2"] || communities["c1"] != communities["c3"] {
		t.Errorf("c-cluster split: %v", communities)
	}
	distinct := map[int]bool{
		communities["a1"]: true,
		communities["b1"]: true,
		communities["c1"]: true,
	}
	if len(distinct) < 3 {
		t.Errorf("expected 3 distinct communities, got %d: %v", len(distinct), communities)
	}
}

// TestLouvainWithResolution verifies that higher resolution produces more communities.
func TestLouvainWithResolution(t *testing.T) {
	t.Parallel()
	// Ring of 8 nodes with local clustering.
	graph := map[string][]string{
		"a": {"b", "h"},
		"b": {"a", "c"},
		"c": {"b", "d"},
		"d": {"c", "e"},
		"e": {"d", "f"},
		"f": {"e", "g"},
		"g": {"f", "h"},
		"h": {"g", "a"},
	}

	low := LouvainWithResolution(graph, 0.5)
	high := LouvainWithResolution(graph, 2.0)

	lowCount := countDistinct(low)
	highCount := countDistinct(high)

	if highCount <= lowCount {
		t.Errorf("expected higher resolution to produce more communities: γ=0.5 gave %d, γ=2.0 gave %d",
			lowCount, highCount)
	}
}

// TestLouvain_OversizedSplit verifies a fully-connected clique of 10 nodes
// gets communities assigned (exercises the split path when needed).
func TestLouvain_OversizedSplit(t *testing.T) {
	t.Parallel()
	nodes := []string{"n0", "n1", "n2", "n3", "n4", "n5", "n6", "n7", "n8", "n9"}
	graph := make(map[string][]string, len(nodes))
	for _, a := range nodes {
		for _, b := range nodes {
			if a != b {
				graph[a] = append(graph[a], b)
			}
		}
	}
	communities := Louvain(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	for _, node := range nodes {
		if _, ok := communities[node]; !ok {
			t.Errorf("node %q missing from result", node)
		}
	}
}
