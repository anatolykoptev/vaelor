package ranking

import (
	"testing"
)

// TestLouvain_TwoClusters verifies two triangles connected by a bridge edge
// land in the same community within each triangle but different communities overall.
func TestLouvain_TwoClusters(t *testing.T) {
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
	result := Louvain(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

// TestLouvain_SingleNode verifies a single node with no edges gets a community assigned.
func TestLouvain_SingleNode(t *testing.T) {
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

// TestLouvain_OversizedSplit verifies a fully-connected clique of 10 nodes
// gets communities assigned (exercises the split path when needed).
func TestLouvain_OversizedSplit(t *testing.T) {
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
