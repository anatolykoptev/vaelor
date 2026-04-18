package callgraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// stubCrossRefs implements graphx.CrossRefs for testing.
type stubCrossRefs struct {
	// handleRoute maps "name|file" → Route.
	handleRoute map[string]graphx.Route
	// fetchedBy maps "method|path" → []SymbolRef.
	fetchedBy map[string][]graphx.SymbolRef
}

func (s *stubCrossRefs) HandlesRoute(_ context.Context, _, name, file string) (graphx.Route, bool, error) {
	key := name + "|" + file
	r, ok := s.handleRoute[key]
	return r, ok, nil
}

func (s *stubCrossRefs) FetchedBy(_ context.Context, _ string, route graphx.Route) ([]graphx.SymbolRef, error) {
	key := route.Method + "|" + route.Path
	return s.fetchedBy[key], nil
}

func (s *stubCrossRefs) TestedBy(_ context.Context, _, _, _ string) ([]graphx.SymbolRef, error) {
	return nil, nil
}

var _ graphx.CrossRefs = (*stubCrossRefs)(nil)

func makeHandlerGraph() (*CallGraph, *parser.Symbol) {
	handler := &parser.Symbol{Name: "handleUsers", Kind: parser.KindFunction, File: "/src/api.go", StartLine: 1, EndLine: 10}
	helper := &parser.Symbol{Name: "queryDB", Kind: parser.KindFunction, File: "/src/db.go", StartLine: 1, EndLine: 20}
	g := &CallGraph{
		Symbols: []*parser.Symbol{handler, helper},
		Edges: []CallEdge{
			{Caller: handler, Callee: helper, CalleeName: "queryDB", Line: 5},
		},
	}
	return g, handler
}

// TestTrace_NilCrossRefs verifies that with nil CrossRefs the output is
// structurally identical to a plain trace (same node count, same tree shape).
func TestTrace_NilCrossRefs(t *testing.T) {
	g, _ := makeHandlerGraph()

	plain := Trace(context.Background(), g, "handleUsers", TraceOpts{Direction: "callees"})
	withNil := Trace(context.Background(), g, "handleUsers", TraceOpts{Direction: "callees", CrossRefs: nil})

	if plain.TotalNodes != withNil.TotalNodes {
		t.Errorf("nil CrossRefs changed TotalNodes: plain=%d withNil=%d", plain.TotalNodes, withNil.TotalNodes)
	}
	if plain.MaxDepth != withNil.MaxDepth {
		t.Errorf("nil CrossRefs changed MaxDepth: plain=%d withNil=%d", plain.MaxDepth, withNil.MaxDepth)
	}
	if len(plain.Tree) != len(withNil.Tree) {
		t.Errorf("nil CrossRefs changed Tree length: plain=%d withNil=%d", len(plain.Tree), len(withNil.Tree))
	}
	if len(plain.Tree[0].Children) != len(withNil.Tree[0].Children) {
		t.Errorf("nil CrossRefs changed children count: plain=%d withNil=%d",
			len(plain.Tree[0].Children), len(withNil.Tree[0].Children))
	}
}

// TestTrace_CrossRefsInjectsFetchNodes verifies that a CrossRefs hook causes a
// synthetic node with Kind==CrossLanguageFetchKind to be appended.
func TestTrace_CrossRefsInjectsFetchNodes(t *testing.T) {
	g, _ := makeHandlerGraph()

	stub := &stubCrossRefs{
		handleRoute: map[string]graphx.Route{
			"handleUsers|/src/api.go": {Method: "GET", Path: "/api/users"},
		},
		fetchedBy: map[string][]graphx.SymbolRef{
			"GET|/api/users": {{Name: "fetchUsers", File: "frontend/api.ts"}},
		},
	}

	result := Trace(context.Background(), g, "handleUsers", TraceOpts{
		Direction: "callees",
		CrossRefs: stub,
		Repo:      "testrepo",
	})

	// handleUsers + queryDB (call graph) + fetchUsers (synthetic) = 3
	if result.TotalNodes != 3 {
		t.Errorf("expected 3 total nodes, got %d", result.TotalNodes)
	}

	root := result.Tree[0]
	var synth *CallChainNode
	for i := range root.Children {
		if root.Children[i].Kind == CrossLanguageFetchKind {
			synth = &root.Children[i]
			break
		}
	}
	if synth == nil {
		t.Fatalf("no %s child found; children: %v", CrossLanguageFetchKind, root.Children)
	}
	if synth.Symbol.Name != "fetchUsers" {
		t.Errorf("synthetic node name: want fetchUsers, got %s", synth.Symbol.Name)
	}
	if synth.Symbol.File != "frontend/api.ts" {
		t.Errorf("synthetic node file: want frontend/api.ts, got %s", synth.Symbol.File)
	}
}
