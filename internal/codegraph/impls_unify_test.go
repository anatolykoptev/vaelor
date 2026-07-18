package codegraph

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestCallEdgesToRels_ConvertsInterfaceEdges(t *testing.T) {
	cg := &callgraph.CallGraph{
		Edges: []callgraph.CallEdge{
			{
				IsInterface: true,
				Caller:      &parser.Symbol{Name: "MyStruct", File: "/repo/main.rs"},
				Callee:      &parser.Symbol{Name: "MyTrait", File: "/repo/trait.rs"},
				Line:        10,
			},
			{
				IsInterface: false,
				Caller:      &parser.Symbol{Name: "funcA", File: "/repo/a.rs"},
				Callee:      &parser.Symbol{Name: "funcB", File: "/repo/b.rs"},
				Line:        5,
			},
		},
	}

	rels := callEdgesToRels(cg)
	if len(rels) != 1 {
		t.Fatalf("expected 1 TypeRelationship (only IsInterface edges), got %d", len(rels))
	}
	if rels[0].Subject != "MyStruct" {
		t.Errorf("expected Subject=MyStruct, got %s", rels[0].Subject)
	}
	if rels[0].Target != "MyTrait" {
		t.Errorf("expected Target=MyTrait, got %s", rels[0].Target)
	}
	if rels[0].Kind != parser.RelImplements {
		t.Errorf("expected Kind=RelImplements, got %v", rels[0].Kind)
	}
}

func TestRemoveImplEdges_KeepsNonInterface(t *testing.T) {
	edges := []callgraph.CallEdge{
		{IsInterface: true, Line: 1},
		{IsInterface: false, Line: 2},
		{IsInterface: true, Line: 3},
		{IsInterface: false, Line: 4},
	}
	filtered := removeImplEdges(edges)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 non-interface edges, got %d", len(filtered))
	}
	for _, ce := range filtered {
		if ce.IsInterface {
			t.Error("found IsInterface edge in filtered result")
		}
	}
}

func TestRemoveImplEdges_Empty(t *testing.T) {
	filtered := removeImplEdges(nil)
	if len(filtered) != 0 {
		t.Errorf("expected 0 edges for nil input, got %d", len(filtered))
	}
}
