package explore

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func mkSym(name, file string) *parser.Symbol {
	return &parser.Symbol{Name: name, File: file, Kind: "function"}
}

// connect builds bidirectional call edges over a fully-connected node set,
// so Louvain reliably places them in one community.
func connect(syms []*parser.Symbol) []callgraph.CallEdge {
	out := make([]callgraph.CallEdge, 0, len(syms)*(len(syms)-1))
	for i := 0; i < len(syms); i++ {
		for j := i + 1; j < len(syms); j++ {
			out = append(out, callgraph.CallEdge{Caller: syms[i], Callee: syms[j]})
			out = append(out, callgraph.CallEdge{Caller: syms[j], Callee: syms[i]})
		}
	}
	return out
}

// TestBuildCommunityOverview_KeepsOnlyNonTrivial asserts that singletons
// and pairs are filtered out of both Count and Clusters. Before the fix,
// Count reported 1000+ Louvain singletons polluting the field.
func TestBuildCommunityOverview_KeepsOnlyNonTrivial(t *testing.T) {
	g1 := []*parser.Symbol{mkSym("g1a", "g1.go"), mkSym("g1b", "g1.go"), mkSym("g1c", "g1.go"), mkSym("g1d", "g1.go")}
	g2 := []*parser.Symbol{mkSym("g2a", "g2.go"), mkSym("g2b", "g2.go"), mkSym("g2c", "g2.go"), mkSym("g2d", "g2.go")}
	pair := []*parser.Symbol{mkSym("pa", "p.go"), mkSym("pb", "p.go")}

	all := append(append([]*parser.Symbol{}, g1...), g2...)
	all = append(all, pair...)

	edges := append(connect(g1), connect(g2)...)
	edges = append(edges, connect(pair)...)

	cg := &callgraph.CallGraph{Symbols: all, Edges: edges}
	overview := buildCommunityOverview(cg, "/repo")
	if overview == nil {
		t.Fatal("expected non-nil overview when ≥2 non-trivial communities exist")
	}

	if overview.Count < 2 {
		t.Fatalf("Count must reflect only non-trivial communities, got %d", overview.Count)
	}
	for _, c := range overview.Clusters {
		if c.Size < minCommunitySize {
			t.Errorf("cluster ID=%d size=%d below minCommunitySize=%d (must be filtered)", c.ID, c.Size, minCommunitySize)
		}
	}
}
