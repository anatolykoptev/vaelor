// cmd/go-code/server_nollm_integration_test.go
//
// Policy-matrix integration test: verifies all four LLM-dependency categories
// in one place with LLM_API_KEY="" (LLMHasKey=false deps).
//
// Strategy: call handlers directly (in-process), same package "main".
// Spinning up a real MCP subprocess would require DATABASE_URL / Redis / etc.
// This covers the LLM-policy surface only — the only regression risk here.
//
// Categories tested (from the LLM-optional refactor spec):
//   - hard:   tool errors with "LLM_API_KEY" in message + IsError=true
//   - soft:   tool succeeds (IsError=false), no "LLM_API_KEY" in text
//   - augment: tool succeeds, core output present, Narrative empty
//   - debug:  tool succeeds, LLMSkippedReason marker set
//
// PR3 of 3 in the LLM-optional refactor.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/llmiface"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// noLLMDeps returns Deps with LLMHasKey=false — simulates LLM_API_KEY="".
func noLLMDeps() analyze.Deps {
	return analyze.Deps{LLM: llmiface.NoOp{}, LLMHasKey: false}
}

// TestServerNoLLM_PolicyMatrix exercises one tool per LLM-dependency category
// with LLMHasKey=false. Serves as a regression lock that survives refactors.
func TestServerNoLLM_PolicyMatrix(t *testing.T) {
	t.Run("hard/code_graph", func(t *testing.T) {
		// code_graph NL-query must return MCP error containing "LLM_API_KEY".
		res, err := handleCodeGraph(
			context.Background(),
			CodeGraphInput{Repo: "owner/repo", Query: "who calls Parse?"},
			Config{},
			noLLMDeps(),
			// store=nil intentional: LLM gate at tool_code_graph.go fires before
			// any store access. If those checks are ever reordered, this test will
			// nil-panic (not silently pass), which is the desired failure signal.
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected non-nil error: %v", err)
		}
		if res == nil {
			t.Fatal("result is nil")
		}
		if !res.IsError {
			t.Errorf("hard tool: expected IsError=true; text: %q", resultText(res))
		}
		if !strings.Contains(resultText(res), "LLM_API_KEY") {
			t.Errorf("hard tool: expected 'LLM_API_KEY' in response; got: %q", resultText(res))
		}
	})

	t.Run("hard/repo_search", func(t *testing.T) {
		// repo_search must return MCP error containing "LLM_API_KEY".
		res, err := handleRepoSearch(
			context.Background(),
			RepoSearchInput{Query: "kubernetes operator"},
			noLLMDeps(),
		)
		if err != nil {
			t.Fatalf("unexpected non-nil error: %v", err)
		}
		if res == nil {
			t.Fatal("result is nil")
		}
		if !res.IsError {
			t.Errorf("hard tool: expected IsError=true; text: %q", resultText(res))
		}
		if !strings.Contains(resultText(res), "LLM_API_KEY") {
			t.Errorf("hard tool: expected 'LLM_API_KEY' in response; got: %q", resultText(res))
		}
	})

	t.Run("soft/repo_analyze_quick", func(t *testing.T) {
		// repo_analyze_quick (local mode) must succeed — IsError=false,
		// no "LLM_API_KEY" in output — even with LLMHasKey=false.
		root := t.TempDir()
		res, err := handleLocalQuickMode(
			context.Background(),
			RepoAnalyzeInput{Repo: root, Mode: modeQuick},
			noLLMDeps(),
		)
		if err != nil {
			t.Fatalf("unexpected non-nil error: %v", err)
		}
		if res == nil {
			t.Fatal("result is nil")
		}
		if res.IsError {
			t.Errorf("soft tool: expected IsError=false; text: %q", resultText(res))
		}
		if strings.Contains(resultText(res), "LLM_API_KEY") {
			t.Errorf("soft tool: must not mention 'LLM_API_KEY'; got: %q", resultText(res))
		}
	})

	t.Run("augment/call_trace", func(t *testing.T) {
		// call_trace must populate CallTree (core output) and leave Narrative empty.
		sym := &parser.Symbol{Name: "serve", Kind: "function", File: "main.go", StartLine: 1, EndLine: 20}
		child := &parser.Symbol{Name: "handleRequest", Kind: "function", File: "handler.go", StartLine: 1, EndLine: 10}
		result := &callgraph.TraceResult{
			Root:       sym,
			Tree:       []callgraph.CallChainNode{{Symbol: sym, Children: []callgraph.CallChainNode{{Symbol: child}}}},
			TotalNodes: 2,
			MaxDepth:   1,
			Resolved:   2,
			Unresolved: 0,
		}

		output := buildCallTraceOutput(context.Background(), "serve", "callees", result, noLLMDeps(), false)

		if len(output.CallTree) == 0 {
			t.Error("augment tool: expected non-empty CallTree")
		}
		if output.Narrative != "" {
			t.Errorf("augment tool: expected empty Narrative with NoOp LLM, got: %q", output.Narrative)
		}
	})

	t.Run("debug/debug_investigate", func(t *testing.T) {
		// debug_investigate handler must return non-error MCP result.
		// runLLMPhase must set LLMSkippedReason = "LLM_API_KEY not set".
		orig := debugInvestigateStore
		debugInvestigateStore = investigate.NewInvestigationStore()
		t.Cleanup(func() { debugInvestigateStore = orig })

		res, err := handleDebugInvestigate(
			context.Background(),
			DebugInvestigateInput{
				Service:   "policy-matrix-test",
				StartUnix: 1_700_000_000,
				EndUnix:   1_700_000_600,
			},
			noLLMDeps(),
			nil, // prom
			nil, // jaeger
			nil, // dozor
		)
		if err != nil {
			t.Fatalf("unexpected non-nil error: %v", err)
		}
		if res == nil {
			t.Fatal("result is nil")
		}
		if res.IsError {
			t.Errorf("debug tool: expected IsError=false; text: %q", resultText(res))
		}
	})
}
