package main

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestCallTrace_NoLLM_NarrativeEmpty verifies that buildCallTraceOutput returns
// core call-tree output with an empty Narrative when LLM is NoOp.
// augment-only category: PR2 of LLM-optional refactor.
//
// Tests generateNarrative's error-swallow path (returns "" on error) so that
// the augment tool's core output is not gated behind LLM availability.
func TestCallTrace_NoLLM_NarrativeEmpty(t *testing.T) {
	deps := analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}

	sym := &parser.Symbol{Name: "main", Kind: "function", File: "main.go", StartLine: 1, EndLine: 10}
	child := &parser.Symbol{Name: "helper", Kind: "function", File: "main.go", StartLine: 12, EndLine: 20}

	// Build a fake TraceResult with TotalNodes > 1 to trigger the Narrative branch.
	result := &callgraph.TraceResult{
		Root:       sym,
		Tree:       []callgraph.CallChainNode{{Symbol: sym, Children: []callgraph.CallChainNode{{Symbol: child}}}},
		TotalNodes: 2,
		MaxDepth:   1,
		Resolved:   2,
		Unresolved: 0,
	}

	output := buildCallTraceOutput(context.Background(), "main", "callees", result, deps, false)

	if len(output.CallTree) == 0 {
		t.Error("expected non-empty CallTree (core output must be present)")
	}
	if output.Narrative != "" {
		t.Errorf("expected empty Narrative with NoOp LLM, got: %q", output.Narrative)
	}
	if output.Symbol != "main" {
		t.Errorf("expected Symbol=main, got: %q", output.Symbol)
	}
}
