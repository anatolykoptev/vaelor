package impact

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

type stubCrossRefs struct {
	tests []graphx.SymbolRef
}

func (s *stubCrossRefs) HandlesRoute(_ context.Context, _, _, _ string) (graphx.Route, bool, error) {
	return graphx.Route{}, false, nil
}

func (s *stubCrossRefs) FetchedBy(_ context.Context, _ string, _ graphx.Route) ([]graphx.SymbolRef, error) {
	return nil, nil
}

func (s *stubCrossRefs) TestedBy(_ context.Context, _, _, _ string) ([]graphx.SymbolRef, error) {
	return s.tests, nil
}

func baseCallGraph() *callgraph.CallGraph {
	target := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "/src/foo.go", StartLine: 1, EndLine: 5}
	return &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target},
		Edges:   []callgraph.CallEdge{},
	}
}

func TestAnalyze_TestsCovering_NilRefs(t *testing.T) {
	t.Parallel()
	result := Analyze(context.Background(), baseCallGraph(), "Foo", Options{})
	if len(result.TestsCovering) != 0 {
		t.Errorf("expected no tests, got %d", len(result.TestsCovering))
	}
}

func TestAnalyze_TestsCovering_EmptyRepo(t *testing.T) {
	t.Parallel()
	refs := &stubCrossRefs{tests: []graphx.SymbolRef{{Name: "TestFoo", File: "foo_test.go"}}}
	result := Analyze(context.Background(), baseCallGraph(), "Foo", Options{Refs: refs})
	if len(result.TestsCovering) != 0 {
		t.Errorf("expected no tests when Repo is empty, got %d", len(result.TestsCovering))
	}
}

func TestAnalyze_TestsCovering_Populated(t *testing.T) {
	t.Parallel()
	refs := &stubCrossRefs{tests: []graphx.SymbolRef{
		{Name: "TestFoo", File: "foo_test.go"},
		{Name: "TestFoo_Edge", File: "foo_test.go"},
	}}
	result := Analyze(context.Background(), baseCallGraph(), "Foo", Options{Refs: refs, Repo: "repo"})
	if len(result.TestsCovering) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(result.TestsCovering))
	}
	if result.TestsCovering[0].Name != "TestFoo" {
		t.Errorf("first test = %q, want TestFoo", result.TestsCovering[0].Name)
	}
}
