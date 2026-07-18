package compound

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// Hard red tests — boundary conditions for compound tools.

func TestFindSymbol_NoMatch(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction},
		{Name: "Bar", Kind: parser.KindStruct}, // not a function
	}
	matches := FindSymbol(symbols, "Baz")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestFindSymbol_SkipsNonFunctions(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindStruct},
		{Name: "Foo", Kind: parser.KindConst},
		{Name: "Foo", Kind: parser.KindVar},
	}
	matches := FindSymbol(symbols, "Foo")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches (no funcs), got %d", len(matches))
	}
}

func TestFindSymbol_EmptySlice(t *testing.T) {
	t.Parallel()
	matches := FindSymbol(nil, "Foo")
	if matches != nil {
		t.Errorf("expected nil for nil input, got %v", matches)
	}
}

func TestUnderstand_NilCallGraph(t *testing.T) {
	t.Parallel()
	// Should not panic with zero-value CallGraph.
	sym := &parser.Symbol{
		Name: "Foo", Kind: parser.KindFunction,
		File: "a.go", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{Tier: "basic"}
	result := Understand(context.Background(), sym, cg, UnderstandOpts{})
	if result.Symbol.Name != "Foo" {
		t.Errorf("expected Foo, got %s", result.Symbol.Name)
	}
	if len(result.Callees) != 0 {
		t.Errorf("expected 0 callees, got %d", len(result.Callees))
	}
}

func TestUnderstand_MaxCalleesRespected(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name: "Hub", Kind: parser.KindFunction,
		File: "a.go", StartLine: 1, EndLine: 50,
	}
	// Create 30 edges from Hub.
	edges := make([]callgraph.CallEdge, 30)
	for i := range edges {
		target := &parser.Symbol{Name: "target", Kind: parser.KindFunction, File: "b.go"}
		edges[i] = callgraph.CallEdge{
			Caller:     sym,
			Callee:     target,
			CalleeName: "target",
			Line:       uint32(i + 1),
		}
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym},
		Edges:   edges,
		Tier:    "basic",
	}

	result := Understand(context.Background(), sym, cg, UnderstandOpts{MaxCallees: 5})
	if len(result.Callees) > 5 {
		t.Errorf("expected <=5 callees, got %d", len(result.Callees))
	}
}

func TestUnderstand_MaxCallersRespected(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name: "Target", Kind: parser.KindFunction,
		File: "a.go", StartLine: 1, EndLine: 5,
	}
	edges := make([]callgraph.CallEdge, 25)
	for i := range edges {
		caller := &parser.Symbol{
			Name: "caller", Kind: parser.KindFunction,
			File: "b.go", StartLine: uint32(i * 10), EndLine: uint32(i*10 + 5),
		}
		edges[i] = callgraph.CallEdge{
			Caller:     caller,
			Callee:     sym,
			CalleeName: "Target",
			Line:       uint32(i + 1),
		}
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym},
		Edges:   edges,
		Tier:    "enhanced",
	}

	result := Understand(context.Background(), sym, cg, UnderstandOpts{IncludeCallers: true, MaxCallers: 3})
	if len(result.Callers) > 3 {
		t.Errorf("expected <=3 callers, got %d", len(result.Callers))
	}
}

func TestUnderstand_CallersDeduplicated(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name: "Target", Kind: parser.KindFunction,
		File: "a.go", StartLine: 1, EndLine: 5,
	}
	caller := &parser.Symbol{
		Name: "Caller", Kind: parser.KindFunction,
		File: "b.go", StartLine: 1, EndLine: 5,
	}
	// Same caller appears twice in edges.
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym, caller},
		Edges: []callgraph.CallEdge{
			{Caller: caller, Callee: sym, CalleeName: "Target", Line: 3},
			{Caller: caller, Callee: sym, CalleeName: "Target", Line: 4},
		},
		Tier: "basic",
	}

	result := Understand(context.Background(), sym, cg, UnderstandOpts{IncludeCallers: true})
	if len(result.Callers) != 1 {
		t.Errorf("expected 1 deduplicated caller, got %d", len(result.Callers))
	}
}

func TestPrepareChange_DeadExportedSymbol(t *testing.T) {
	t.Parallel()
	// Exported symbol with zero callers — should detect as dead
	// when IncludeExported=true (which PrepareChange sets).
	sym := &parser.Symbol{
		Name: "ExportedFunc", Kind: parser.KindFunction,
		File: "a.go", StartLine: 1, EndLine: 10,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym},
		Edges:   nil,
		Tier:    "basic",
	}

	result := PrepareChange(context.Background(), cg, "ExportedFunc", PrepareChangeOpts{})
	if !result.Found {
		t.Fatal("expected symbol found")
	}
	if !result.IsDead {
		t.Error("exported symbol with no callers should be dead (IncludeExported=true)")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about dead symbol")
	}
}

func TestPrepareChange_ZeroDepthDefault(t *testing.T) {
	t.Parallel()
	// MaxDepth=0 should use default (5), not cause infinite traversal.
	foo := &parser.Symbol{Name: "foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1, EndLine: 5}
	bar := &parser.Symbol{Name: "bar", Kind: parser.KindFunction, File: "b.go", StartLine: 1, EndLine: 5}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{foo, bar},
		Edges:   []callgraph.CallEdge{{Caller: bar, Callee: foo, CalleeName: "foo", Line: 3}},
		Tier:    "enhanced",
	}

	result := PrepareChange(context.Background(), cg, "foo", PrepareChangeOpts{MaxDepth: 0})
	if !result.Found {
		t.Fatal("expected symbol found")
	}
	if result.Impact.TotalAffected == 0 {
		t.Error("expected affected callers")
	}
	if result.Tier != "enhanced" {
		t.Errorf("expected enhanced tier, got %s", result.Tier)
	}
}

func TestPrepareChange_SymbolInfoPopulated(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name: "Process", Kind: parser.KindMethod,
		File: "handler.go", StartLine: 10, EndLine: 50,
		Signature:  "func (h *Handler) Process(ctx context.Context) error",
		Complexity: 12,
		Receiver:   "Handler",
	}
	caller := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 5}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym, caller},
		Edges:   []callgraph.CallEdge{{Caller: caller, Callee: sym, CalleeName: "Process", Line: 3}},
		Tier:    "enhanced",
	}

	result := PrepareChange(context.Background(), cg, "Process", PrepareChangeOpts{})
	if result.Symbol.Name != "Process" {
		t.Errorf("expected Process, got %s", result.Symbol.Name)
	}
	if result.Symbol.Signature == "" {
		t.Error("expected signature populated")
	}
	if result.Symbol.Complexity != 12 {
		t.Errorf("expected complexity 12, got %d", result.Symbol.Complexity)
	}
	if result.Symbol.Receiver != "Handler" {
		t.Errorf("expected receiver Handler, got %s", result.Symbol.Receiver)
	}
}
