package main

import (
	"context"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-kit/llm"
)

// TestDebugInvestigate_NoLLM_RunsAndSetsMarker verifies that debug_investigate
// proceeds without LLM and surfaces a clear marker in the result diagnostics.
//
// With LLMHasKey=false, handleDebugInvestigate must NOT return an MCP error —
// deterministic phases (trace analysis, metric spikes, alert violations,
// hypothesis ranking) all work without an API key. Only the inner LLM phase
// is skipped, and it must set Diagnostics.LLMSkippedReason to the expected string.
//
// Test strategy: call runLLMPhase directly (unit-level) with LLMHasKey=false
// and a non-empty result, and assert the reason is set. A separate integration
// path (tool_debug_investigate_integration_test.go) covers the end-to-end store.
//
// Uses NoOp{} not nil — avoids the typed-nil interface trap (PR2 reviewer concern).
func TestDebugInvestigate_NoLLM_RunsAndSetsMarker(t *testing.T) {
	deps := analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
	res := &investigate.InvestigationResult{
		Service: "go-code",
		Hypotheses: []investigate.Hypothesis{
			{Subject: "HandleRequest", AnomalyScore: 0.8},
		},
	}
	input := DebugInvestigateInput{
		Service:   "go-code",
		StartUnix: 1_000_000,
		EndUnix:   1_001_000,
	}

	runLLMPhase(
		context.Background(),
		deps,
		nil, // metricNames
		input,
		nil, // services
		nil, // ops
		time.Unix(1_000_000, 0),
		time.Unix(1_001_000, 0),
		res,
	)

	if res.Diagnostics.LLMSkippedReason != llmSkipReasonNoKey {
		t.Errorf("LLMSkippedReason = %q, want %q", res.Diagnostics.LLMSkippedReason, llmSkipReasonNoKey)
	}
	// LLMSummary must be empty — no LLM was called.
	if res.LLMSummary != "" {
		t.Errorf("LLMSummary should be empty when LLM is skipped, got: %q", res.LLMSummary)
	}
}

// TestDebugInvestigate_NoLLM_HandlerDoesNotError verifies that
// handleDebugInvestigate returns a non-error MCP result when LLMHasKey=false.
// The outer gate was removed (PR2 follow-up): deterministic output is valuable
// even without an API key.
//
// With nil prom/jaeger the background investigation will fail quickly, but the
// handler's immediate return ("Investigation started") must NOT be an MCP error.
func TestDebugInvestigate_NoLLM_HandlerDoesNotError(t *testing.T) {
	// Reset store so this test gets a fresh slot.
	orig := debugInvestigateStore
	debugInvestigateStore = investigate.NewInvestigationStore()
	t.Cleanup(func() { debugInvestigateStore = orig })

	deps := analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
	input := DebugInvestigateInput{
		Service:   "go-code-nollm",
		StartUnix: 1_000_000,
		EndUnix:   1_001_000,
	}
	res, err := handleDebugInvestigate(context.Background(), input, Config{}, deps, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		t.Errorf("expected IsError=false, got true; text: %q", resultText(res))
	}

	// C9 race fix: handleDebugInvestigate launches a background goroutine
	// (runInvestigation) that writes to debugInvestigateStore via Fail/Finish.
	// t.Cleanup (above) writes debugInvestigateStore = orig. Without this wait,
	// the cleanup assignment races the goroutine's store mutation.
	// Poll until the goroutine reaches Done or Failed, then cleanup is safe.
	start := time.Unix(1_000_000, 0)
	end := time.Unix(1_001_000, 0)
	if st := pollStore("go-code-nollm", start, end, "" /* repo */, 3*time.Second); st == nil {
		t.Log("warn: pollStore timed out — background investigation did not finish in 3s")
	}
}
