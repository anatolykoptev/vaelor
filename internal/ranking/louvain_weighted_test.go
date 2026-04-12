package ranking

import "testing"

// TestLouvainWeighted_StrongEdgesCluster verifies that weighted edges affect clustering.
func TestLouvainWeighted_StrongEdgesCluster(t *testing.T) {
	graph := map[string]map[string]int{
		"a": {"b": 5},
		"b": {"a": 5, "c": 1},
		"c": {"b": 1, "d": 5},
		"d": {"c": 5},
	}
	communities := LouvainWeighted(graph)
	if communities == nil {
		t.Fatal("expected non-nil result")
	}
	if communities["a"] != communities["b"] {
		t.Errorf("expected a and b in same community, got %v", communities)
	}
	if communities["c"] != communities["d"] {
		t.Errorf("expected c and d in same community, got %v", communities)
	}
	if communities["a"] == communities["c"] {
		t.Errorf("expected different communities for {a,b} and {c,d}, got %v", communities)
	}
}

func TestLouvainWeighted_Empty(t *testing.T) {
	if LouvainWeighted(nil) != nil {
		t.Error("expected nil for nil input")
	}
}
