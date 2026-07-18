package compound_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestPrepareChange_Basic(t *testing.T) {
	t.Parallel()
	target := makeFunc("ProcessOrder", "order.go", 10, 40)
	caller1 := makeFunc("HandleRequest", "handler.go", 5, 30)
	caller2 := makeFunc("RunBatch", "batch.go", 15, 50)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target, caller1, caller2},
		Edges: []callgraph.CallEdge{
			{Caller: caller1, Callee: target, CalleeName: "ProcessOrder", Line: 20},
			{Caller: caller2, Callee: target, CalleeName: "ProcessOrder", Line: 35},
		},
		Tier: "basic",
	}

	result := compound.PrepareChange(context.Background(), cg, "ProcessOrder", compound.PrepareChangeOpts{})

	if !result.Found {
		t.Fatal("expected found=true for existing symbol")
	}
	if result.Symbol.Name != "ProcessOrder" {
		t.Errorf("expected symbol name ProcessOrder, got %s", result.Symbol.Name)
	}
	if result.Impact == nil {
		t.Fatal("expected impact result to be populated")
	}
	if result.Impact.TotalAffected == 0 {
		t.Error("expected at least one affected caller")
	}
	if result.IsDead {
		t.Error("expected is_dead=false for symbol with callers")
	}
	if result.Tier != "basic" {
		t.Errorf("expected tier basic, got %s", result.Tier)
	}
}

func TestPrepareChange_NotFound(t *testing.T) {
	t.Parallel()
	sym := makeFunc("existingFunc", "file.go", 1, 10)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym},
		Edges:   nil,
		Tier:    "basic",
	}

	result := compound.PrepareChange(context.Background(), cg, "missingSymbol", compound.PrepareChangeOpts{})

	if result.Found {
		t.Error("expected found=false for missing symbol")
	}
	if result.DeadCode != nil {
		t.Error("expected dead_code to be nil when symbol not found")
	}
	if result.Symbol.Name != "" {
		t.Error("expected empty symbol when not found")
	}
}

func TestPrepareChange_IsDead(t *testing.T) {
	t.Parallel()
	// unexported symbol with no callers → should appear in dead code
	orphan := &parser.Symbol{
		Name:      "orphanHelper",
		Kind:      parser.KindFunction,
		File:      "internal.go",
		StartLine: 5,
		EndLine:   15,
	}
	other := makeFunc("MainEntry", "main.go", 1, 5)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{orphan, other},
		Edges:   nil,
		Tier:    "basic",
	}

	result := compound.PrepareChange(context.Background(), cg, "orphanHelper", compound.PrepareChangeOpts{})

	if !result.Found {
		t.Fatal("expected found=true for existing symbol")
	}
	if !result.IsDead {
		t.Error("expected is_dead=true for unexported symbol with no callers")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected at least one warning for dead symbol")
	}
}
