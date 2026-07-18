package compound_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func makeFunc(name, file string, start, end uint32) *parser.Symbol {
	return &parser.Symbol{
		Name:      name,
		Kind:      parser.KindFunction,
		File:      file,
		StartLine: start,
		EndLine:   end,
	}
}

func TestUnderstand_Basic(t *testing.T) {
	t.Parallel()
	caller := makeFunc("HandleRequest", "handler.go", 10, 30)
	callee1 := makeFunc("parseBody", "handler.go", 35, 45)
	callee2 := makeFunc("writeJSON", "response.go", 5, 15)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{caller, callee1, callee2},
		Edges: []callgraph.CallEdge{
			{Caller: caller, Callee: callee1, CalleeName: "parseBody", Line: 15},
			{Caller: caller, Callee: callee2, CalleeName: "writeJSON", Line: 20},
		},
		Tier: "basic",
	}

	result := compound.Understand(context.Background(), caller, cg, compound.UnderstandOpts{})

	if result.Symbol.Name != "HandleRequest" {
		t.Errorf("expected symbol name HandleRequest, got %s", result.Symbol.Name)
	}
	if result.Symbol.Kind != "function" {
		t.Errorf("expected kind function, got %s", result.Symbol.Kind)
	}
	if result.Tier != "basic" {
		t.Errorf("expected tier basic, got %s", result.Tier)
	}
	if len(result.Callees) != 2 {
		t.Errorf("expected 2 callees, got %d", len(result.Callees))
	}
	if result.Callees[0].Name != "parseBody" {
		t.Errorf("expected first callee parseBody, got %s", result.Callees[0].Name)
	}
	if len(result.Callers) != 0 {
		t.Errorf("expected no callers (IncludeCallers=false), got %d", len(result.Callers))
	}
}

func TestUnderstand_Ambiguous(t *testing.T) {
	t.Parallel()
	sym1 := makeFunc("Process", "pkg/a/process.go", 1, 10)
	sym2 := makeFunc("Process", "pkg/b/process.go", 1, 10)
	other := makeFunc("Other", "pkg/c/other.go", 1, 5)

	symbols := []*parser.Symbol{sym1, sym2, other}

	matches := compound.FindSymbol(symbols, "Process")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for Process, got %d", len(matches))
	}

	noMatches := compound.FindSymbol(symbols, "Missing")
	if len(noMatches) != 0 {
		t.Errorf("expected 0 matches for Missing, got %d", len(noMatches))
	}

	exactMatch := compound.FindSymbol(symbols, "Other")
	if len(exactMatch) != 1 {
		t.Errorf("expected 1 match for Other, got %d", len(exactMatch))
	}
}

func TestUnderstand_WithCallers(t *testing.T) {
	t.Parallel()
	target := makeFunc("doWork", "worker.go", 50, 80)
	caller1 := makeFunc("RunJob", "job.go", 10, 40)
	caller2 := makeFunc("RunScheduled", "scheduler.go", 5, 20)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target, caller1, caller2},
		Edges: []callgraph.CallEdge{
			{Caller: caller1, Callee: target, CalleeName: "doWork", Line: 25},
			{Caller: caller2, Callee: target, CalleeName: "doWork", Line: 12},
		},
		Tier: "enhanced",
	}

	result := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{
		IncludeCallers: true,
	})

	if len(result.Callers) != 2 {
		t.Fatalf("expected 2 callers, got %d", len(result.Callers))
	}

	names := map[string]bool{
		result.Callers[0].Name: true,
		result.Callers[1].Name: true,
	}
	if !names["RunJob"] || !names["RunScheduled"] {
		t.Errorf("unexpected caller names: %v", names)
	}
	if len(result.Callees) != 0 {
		t.Errorf("expected 0 callees for doWork, got %d", len(result.Callees))
	}
}
