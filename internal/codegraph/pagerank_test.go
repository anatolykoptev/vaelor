package codegraph

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestComputeSymbolPageRank(t *testing.T) {
	t.Parallel()

	symA := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/repo/cmd/main.go"}
	symB := &parser.Symbol{Name: "handler", Kind: parser.KindFunction, File: "/repo/internal/handler.go"}
	symC := &parser.Symbol{Name: "helper", Kind: parser.KindFunction, File: "/repo/internal/helper.go"}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{symA, symB, symC},
		Edges: []callgraph.CallEdge{
			{Caller: symA, Callee: symB},
			{Caller: symA, Callee: symC},
			{Caller: symB, Callee: symC},
		},
	}

	scores := computeSymbolPageRank("/repo", cg.Symbols, cg)
	if scores == nil {
		t.Fatal("expected non-nil scores")
	}

	// helper has most incoming edges (called by main and handler).
	helperKey := "helper" + compositeKeyDelim + "internal/helper.go"
	handlerKey := "handler" + compositeKeyDelim + "internal/handler.go"

	if scores[helperKey] <= scores[handlerKey] {
		t.Errorf("helper (%.4f) should rank higher than handler (%.4f)",
			scores[helperKey], scores[handlerKey])
	}
}

func TestComputeSymbolPageRankEmpty(t *testing.T) {
	t.Parallel()
	cg := &callgraph.CallGraph{}
	scores := computeSymbolPageRank("/repo", nil, cg)
	if scores != nil {
		t.Errorf("expected nil for empty graph, got %v", scores)
	}
}
