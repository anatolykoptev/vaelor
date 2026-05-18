package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-kit/llm"
)

// TestCodeGraph_NoLLM_ReturnsExplicitError verifies that code_graph returns
// an explicit MCP error containing "LLM_API_KEY" when LLMHasKey is false.
// Hard-tool gate: PR2 of LLM-optional refactor.
func TestCodeGraph_NoLLM_ReturnsExplicitError(t *testing.T) {
	deps := analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
	input := CodeGraphInput{
		Repo:  "owner/repo",
		Query: "who calls ParseFile?",
	}
	res, err := handleCodeGraph(context.Background(), input, Config{}, deps, nil)
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
