// cmd/go-code/tool_debug_investigate_gamma_e_test.go
// Tests for Phase γ.E: LLM cache + structured next_check.
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/llm"

	"github.com/anatolykoptev/vaelor/internal/investigate"
)

// ---------- γ.E.1: cache key tests ----------

// TestInvestigationCacheKey_Stable verifies same input → same key.
func TestInvestigationCacheKey_Stable(t *testing.T) {
	input := DebugInvestigateInput{
		Service:   "payments",
		StartUnix: 1000,
		EndUnix:   2000,
	}
	top := []investigate.Hypothesis{
		{Subject: "HandlePayment", AnomalyScore: 0.9},
		{Subject: "ProcessRefund", AnomalyScore: 0.7},
	}
	k1 := investigationCacheKey(input, top)
	k2 := investigationCacheKey(input, top)
	if k1 != k2 {
		t.Errorf("cache key not stable: %q vs %q", k1, k2)
	}
	if !strings.HasPrefix(k1, "investigate:llm:") {
		t.Errorf("cache key missing prefix: %q", k1)
	}
}

// TestInvestigationCacheKey_DifferentInputs verifies different service → different keys.
func TestInvestigationCacheKey_DifferentInputs(t *testing.T) {
	input1 := DebugInvestigateInput{Service: "svc-a", StartUnix: 1000, EndUnix: 2000}
	input2 := DebugInvestigateInput{Service: "svc-b", StartUnix: 1000, EndUnix: 2000}
	top := []investigate.Hypothesis{{Subject: "X", AnomalyScore: 0.5}}
	k1 := investigationCacheKey(input1, top)
	k2 := investigationCacheKey(input2, top)
	if k1 == k2 {
		t.Errorf("different services should produce different keys: both %q", k1)
	}
}

// TestInvestigationCacheKey_TopFiveBoundary verifies only first 5 hypotheses affect key.
func TestInvestigationCacheKey_TopFiveBoundary(t *testing.T) {
	input := DebugInvestigateInput{Service: "svc", StartUnix: 100, EndUnix: 200}
	top6 := []investigate.Hypothesis{
		{Subject: "A", AnomalyScore: 0.9},
		{Subject: "B", AnomalyScore: 0.8},
		{Subject: "C", AnomalyScore: 0.7},
		{Subject: "D", AnomalyScore: 0.6},
		{Subject: "E", AnomalyScore: 0.5},
		{Subject: "F_ignored", AnomalyScore: 0.4},
	}
	top5 := top6[:5]
	kWith6 := investigationCacheKey(input, top6)
	kWith5 := investigationCacheKey(input, top5)
	if kWith6 != kWith5 {
		t.Errorf("6th hypothesis should not affect key: %q vs %q", kWith6, kWith5)
	}

	// Changing the 6th should still produce the same key.
	top6b := make([]investigate.Hypothesis, 6)
	copy(top6b, top6)
	top6b[5].Subject = "G_also_ignored"
	top6b[5].AnomalyScore = 0.1
	kWith6b := investigationCacheKey(input, top6b)
	if kWith6 != kWith6b {
		t.Errorf("changed 6th hypothesis affects key: %q vs %q", kWith6, kWith6b)
	}
}

// ---------- γ.E.1: Phase 5 cache integration tests ----------

// fakePanicLLM panics if Complete is called — asserts LLM is NOT called on cache hit.
type fakePanicLLM struct{}

func (f *fakePanicLLM) Complete(_ context.Context, _, _ string, _ ...llm.ChatOption) (string, error) {
	panic("LLM.Complete must not be called on cache hit")
}

// fakeCaptureLLM records whether Complete was called and returns a preset summary.
type fakeCaptureLLM struct {
	called  bool
	summary string
}

func (f *fakeCaptureLLM) Complete(_ context.Context, _, _ string, _ ...llm.ChatOption) (string, error) {
	f.called = true
	return f.summary, nil
}

// newTestCache creates an in-memory kitcache suitable for tests.
func newTestCache(t *testing.T) *kitcache.Cache {
	t.Helper()
	return kitcache.New(kitcache.Config{
		L1MaxItems: 100,
		L1TTL:      time.Minute,
	})
}

// TestPhase5_CacheHit_SkipsLLM verifies a pre-populated cache prevents LLM call.
func TestPhase5_CacheHit_SkipsLLM(t *testing.T) {
	tc := newTestCache(t)
	input := DebugInvestigateInput{Service: "svc", StartUnix: 100, EndUnix: 200}
	top := []investigate.Hypothesis{
		{Subject: "HandleReq", AnomalyScore: 0.8},
	}
	res := &investigate.InvestigationResult{
		Service:    "svc",
		Hypotheses: top,
	}

	// Pre-populate cache with the key that runLLMPhaseInner will compute.
	cacheKey := investigationCacheKey(input, top)
	if err := kitcache.SetJSONWithTTL(tc, context.Background(), cacheKey, "cached summary", time.Minute); err != nil {
		t.Fatalf("pre-populate cache: %v", err)
	}

	// Must not panic (fake LLM panics if called).
	runLLMPhaseInner(context.Background(), &fakePanicLLM{}, tc, nil, input, nil, nil, res.Range.Start, res.Range.End, res)

	if res.LLMSummary != "cached summary" {
		t.Errorf("LLMSummary = %q, want %q", res.LLMSummary, "cached summary")
	}
	if !res.Diagnostics.LLMCacheHit {
		t.Error("LLMCacheHit should be true on cache hit")
	}
}

// TestPhase5_CacheMiss_CallsLLMAndCaches verifies LLM is called on miss and result cached.
func TestPhase5_CacheMiss_CallsLLMAndCaches(t *testing.T) {
	tc := newTestCache(t)
	fakeLLM := &fakeCaptureLLM{summary: "llm response"}
	input := DebugInvestigateInput{Service: "svc2", StartUnix: 300, EndUnix: 400}
	top := []investigate.Hypothesis{
		{Subject: "ProcessOrder", AnomalyScore: 0.6},
	}
	res := &investigate.InvestigationResult{
		Service:    "svc2",
		Hypotheses: top,
	}

	runLLMPhaseInner(context.Background(), fakeLLM, tc, nil, input, nil, nil, res.Range.Start, res.Range.End, res)

	if !fakeLLM.called {
		t.Error("LLM.Complete should have been called on cache miss")
	}
	if res.LLMSummary != "llm response" {
		t.Errorf("LLMSummary = %q, want %q", res.LLMSummary, "llm response")
	}
	if res.Diagnostics.LLMCacheHit {
		t.Error("LLMCacheHit should be false on cache miss")
	}

	// Verify result is now cached.
	cacheKey := investigationCacheKey(input, top)
	cached, ok, err := kitcache.GetJSON[string](tc, context.Background(), cacheKey)
	if err != nil || !ok {
		t.Error("expected result to be cached after LLM call")
	} else if cached != "llm response" {
		t.Errorf("cached value = %q, want %q", cached, "llm response")
	}
}

// ---------- γ.E.2: next_check format tests ----------

// TestNextCheck_FormatRender verifies structured NextChecks render as XML.
func TestNextCheck_FormatRender(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject: "HandleMessage",
				NextChecks: []investigate.NextCheck{
					{Tool: "understand", Args: map[string]string{"symbol": "HandleMessage", "repo": "/src/go-code"}},
					{Tool: "code_health", Args: map[string]string{"repo": "/src/go-code"}},
				},
			},
		},
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, `tool="understand"`) {
		t.Errorf("expected tool attribute understand in output:\n%s", out)
	}
	if !strings.Contains(out, `tool="code_health"`) {
		t.Errorf("expected tool attribute code_health in output:\n%s", out)
	}
	if !strings.Contains(out, `<arg name="symbol">HandleMessage</arg>`) {
		t.Errorf("expected symbol arg in output:\n%s", out)
	}
	if !strings.Contains(out, `<arg name="repo">/src/go-code</arg>`) {
		t.Errorf("expected repo arg in output:\n%s", out)
	}
}

// TestNextCheck_EmptyArgs_RendersToolOnly verifies tool with no args renders correctly.
func TestNextCheck_EmptyArgs_RendersToolOnly(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject: "SomeFunc",
				NextChecks: []investigate.NextCheck{
					{Tool: "code_health"},
				},
			},
		},
	}
	out := formatInvestigationResult(r)
	if !strings.Contains(out, `tool="code_health"`) {
		t.Errorf("expected tool=code_health in output:\n%s", out)
	}
	// No <arg> elements when Args is empty.
	if strings.Contains(out, "<arg") {
		t.Errorf("unexpected <arg> element when Args is empty:\n%s", out)
	}
}

// TestInvestigationCacheKey_FusedScoreInfluence verifies that different FusedScores
// produce different cache keys even when AnomalyScore is identical.
func TestInvestigationCacheKey_FusedScoreInfluence(t *testing.T) {
	input := DebugInvestigateInput{Service: "svc", StartUnix: 100, EndUnix: 200}
	// Same AnomalyScore, different FusedScore — must produce different key.
	topA := []investigate.Hypothesis{
		{Subject: "HandlePayment", AnomalyScore: 0.9, FusedScore: 0.75},
	}
	topB := []investigate.Hypothesis{
		{Subject: "HandlePayment", AnomalyScore: 0.9, FusedScore: 0.42},
	}
	kA := investigationCacheKey(input, topA)
	kB := investigationCacheKey(input, topB)
	if kA == kB {
		t.Errorf("different FusedScores must produce different keys; both produced %q", kA)
	}
}

// TestInvestigationCacheKey_DifferentRepoProducesDifferentKey verifies that
// same service+window+hypotheses but different repo produces a different key.
// This is the regression test for the empirical bug: re-running with corrected
// repo arg returned the prior (wrong-repo) cached result.
func TestInvestigationCacheKey_DifferentRepoProducesDifferentKey(t *testing.T) {
	top := []investigate.Hypothesis{
		{Subject: "HandleRequest", AnomalyScore: 0.9, FusedScore: 0.8},
	}
	inputA := DebugInvestigateInput{Service: "svc", StartUnix: 1000, EndUnix: 2000, Repo: "anatolykoptev/acme-sfu"}
	inputB := DebugInvestigateInput{Service: "svc", StartUnix: 1000, EndUnix: 2000, Repo: "anatolykoptev/acme-edge"}
	kA := investigationCacheKey(inputA, top)
	kB := investigationCacheKey(inputB, top)
	if kA == kB {
		t.Errorf("different repos must produce different keys; both produced %q", kA)
	}
}
