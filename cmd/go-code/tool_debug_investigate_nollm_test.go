package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/llmiface"
)

// TestDebugInvestigate_NoLLM_ReturnsExplicitError verifies that debug_investigate
// returns an explicit MCP error containing "LLM_API_KEY" when LLMHasKey is false.
// Covers the typed-nil interface trap: uses NoOp{} not nil.
// Hard-tool gate: PR2 of LLM-optional refactor.
func TestDebugInvestigate_NoLLM_ReturnsExplicitError(t *testing.T) {
	deps := analyze.Deps{
		LLM:       llmiface.NoOp{},
		LLMHasKey: false,
	}
	// Valid input: non-empty service, non-zero window, valid hint_kind.
	input := DebugInvestigateInput{
		Service:   "go-code",
		StartUnix: 1_000_000,
		EndUnix:   1_001_000,
	}
	res, err := handleDebugInvestigate(context.Background(), input, deps, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	text := resultText(res)
	if !strings.Contains(text, "LLM_API_KEY") {
		t.Errorf("expected error mentioning LLM_API_KEY, got: %q", text)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true, got false; text: %q", text)
	}
}
