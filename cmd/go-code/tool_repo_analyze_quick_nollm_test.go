package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-kit/llm"
)

// TestRepoAnalyzeQuick_NoLLM_SoftFallback verifies that handleLocalQuickMode
// returns a non-error MCP result even when LLMHasKey is false.
// soft-fallback category: PR2 of LLM-optional refactor.
//
// Uses a local tmpdir; handleLocalQuickMode does not call LLM directly, so
// this confirms the soft path doesn't break with NoOp deps.
func TestRepoAnalyzeQuick_NoLLM_SoftFallback(t *testing.T) {
	root := t.TempDir()

	deps := analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
	input := RepoAnalyzeInput{
		Repo: root,
		Mode: modeQuick,
	}
	res, err := handleLocalQuickMode(context.Background(), input, deps)
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		t.Errorf("soft tool must not return IsError=true; text: %q", resultText(res))
	}
	text := resultText(res)
	if strings.Contains(text, "LLM_API_KEY") {
		t.Errorf("soft tool must not mention LLM_API_KEY; text: %q", text)
	}
}
