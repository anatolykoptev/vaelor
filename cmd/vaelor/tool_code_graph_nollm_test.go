package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
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

// TestCodeGraph_NoLLM_ExplicitTemplateBypassesGate: with no LLM key, a
// natural-language query is refused with a pointer to template, while an
// explicit template is validated instead of hitting the LLM gate.
func TestCodeGraph_NoLLM_ExplicitTemplateBypassesGate(t *testing.T) {
	deps := analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}

	res, _ := handleCodeGraph(context.Background(), CodeGraphInput{Repo: "owner/repo", Query: "who calls ParseFile?"}, Config{}, deps, nil)
	if text := resultText(res); !res.IsError || !strings.Contains(text, "who_calls(name)") {
		t.Errorf("NL query without LLM: want error listing templates, got %q", text)
	}

	for _, tc := range []struct {
		name, template, want string
		params               map[string]string
	}{
		{"unknown template", "who_callz", "unknown template", nil},
		{"missing param", "who_calls", `requires param "name"`, nil},
		{"param typo", "who_calls", `does not take param "fn"`, map[string]string{"fn": "X"}},
	} {
		res, err := handleCodeGraph(context.Background(), CodeGraphInput{Repo: "owner/repo", Template: tc.template, Params: tc.params}, Config{}, deps, nil)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		text := resultText(res)
		if !res.IsError || !strings.Contains(text, tc.want) || strings.Contains(text, "LLM_API_KEY") {
			t.Errorf("%s: want validation error %q (not the LLM gate), got %q", tc.name, tc.want, text)
		}
	}
}

// TestCodeGraphPrecheck_ExplicitTemplateNeedsNoLLM: handleCodeGraph runs this
// precheck before any repo work; a valid template must pass with no LLM key,
// and the template must reach QueryGraph as the explicit classification.
func TestCodeGraphPrecheck_ExplicitTemplateNeedsNoLLM(t *testing.T) {
	deps := analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}
	input := CodeGraphInput{Repo: "owner/repo", Template: "call_chain", Params: map[string]string{"from": "main", "to": "Serve"}}

	explicit, refusal := codeGraphPrecheck(&input, deps)
	if refusal != nil {
		t.Fatalf("valid template refused without LLM: %q", resultText(refusal))
	}
	if explicit == nil || explicit.Template != "call_chain" || explicit.Params["to"] != "Serve" {
		t.Fatalf("explicit = %+v, want call_chain from=main to=Serve", explicit)
	}
	if input.Query != "call_chain from=main to=Serve" {
		t.Errorf("empty query not filled from template: %q", input.Query)
	}
}
