package research

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestExpandFromCallGraph_callers(t *testing.T) {
	t.Parallel()
	foo := &parser.Symbol{Name: "Foo", File: "foo.go", Kind: parser.KindFunction}
	bar := &parser.Symbol{Name: "Bar", File: "bar.go", Kind: parser.KindFunction}
	baz := &parser.Symbol{Name: "Baz", File: "baz.go", Kind: parser.KindFunction}

	g := &callgraph.CallGraph{
		Edges: []callgraph.CallEdge{
			{Caller: bar, Callee: foo, CalleeName: "Foo"}, // bar → foo
			{Caller: baz, Callee: bar, CalleeName: "Bar"}, // baz → bar
		},
	}

	seedFiles := map[string]bool{"foo.go": true}
	got := expandFromCallGraph(seedFiles, g, 2)
	dists := map[string]int{}
	for _, r := range got {
		dists[r.relPath] = r.distance
	}

	if dists["bar.go"] != 1 {
		t.Errorf("bar.go must be at distance 1 (direct caller of foo), got %d", dists["bar.go"])
	}
	if dists["baz.go"] != 2 {
		t.Errorf("baz.go must be at distance 2 (caller of caller), got %d", dists["baz.go"])
	}
	if _, ok := dists["foo.go"]; ok {
		t.Error("seed foo.go must NOT be in expand output")
	}
}

func TestExpandFromCallGraph_callees(t *testing.T) {
	t.Parallel()
	foo := &parser.Symbol{Name: "Foo", File: "foo.go", Kind: parser.KindFunction}
	helper := &parser.Symbol{Name: "Helper", File: "util.go", Kind: parser.KindFunction}

	g := &callgraph.CallGraph{
		Edges: []callgraph.CallEdge{
			{Caller: foo, Callee: helper, CalleeName: "Helper"}, // foo → util.go
		},
	}

	seedFiles := map[string]bool{"foo.go": true}
	got := expandFromCallGraph(seedFiles, g, 2)
	found := false
	for _, r := range got {
		if r.relPath == "util.go" && r.distance == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("util.go (callee of foo) must be at distance 1, got %+v", got)
	}
}

func TestExpandFromCallGraph_nilSafe(t *testing.T) {
	t.Parallel()
	if got := expandFromCallGraph(map[string]bool{"x": true}, nil, 2); got != nil {
		t.Errorf("expected nil for nil graph, got %+v", got)
	}
	if got := expandFromCallGraph(map[string]bool{"x": true}, &callgraph.CallGraph{}, 0); len(got) != 0 {
		t.Errorf("expected empty for zero hops, got %+v", got)
	}
}

func TestMergeExpandResultsKeepsShorterDistance(t *testing.T) {
	t.Parallel()
	a := []expandResult{{relPath: "x.go", distance: 3, whyLinked: "imports foo"}}
	b := []expandResult{{relPath: "x.go", distance: 1, whyLinked: "calls Foo"}}
	out := mergeExpandResults(a, b)
	if len(out) != 1 {
		t.Fatalf("expected dedup, got %d", len(out))
	}
	if out[0].distance != 1 || out[0].whyLinked != "calls Foo" {
		t.Errorf("expected shorter-distance entry to win, got %+v", out[0])
	}
}
