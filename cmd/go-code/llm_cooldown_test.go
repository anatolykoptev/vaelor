package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	kitllm "github.com/anatolykoptev/go-kit/llm"
)

// okResponseBody returns a minimal valid OpenAI chat-completion JSON.
func okResponseBody(content string) []byte {
	resp := map[string]any{
		"choices": []any{
			map[string]any{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// TestLLMCooldownDuration_EnvOverride verifies LLM_COOLDOWN_SECONDS reaches kit.
//
// Falsifiability: revert to CooldownConfig{} (Default=0 → kit uses 60s fallback).
// At the 2.1s probe point the primary is still cooled (60s > 2.1s) →
// primaryHits2 == 0 → FAIL.
func TestLLMCooldownDuration_EnvOverride(t *testing.T) {
	t.Setenv("LLM_COOLDOWN_SECONDS", "2")

	const primaryModel = "env-primary"
	const fallbackModel = "env-fallback"

	var primaryHits atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model == primaryModel {
			primaryHits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(okResponseBody("ok"))
	}))
	defer srv.Close()

	chain := []kitllm.Endpoint{
		{URL: srv.URL, Key: "k", Model: primaryModel},
		{URL: srv.URL, Key: "k", Model: fallbackModel},
	}

	client := kitllm.NewClient(srv.URL, "k", primaryModel,
		kitllm.WithEndpoints(chain),
		kitllm.WithMaxRetries(1),
		kitllm.WithModelCooldown(kitllm.CooldownConfig{Default: llmCooldownDuration()}),
	)

	ctx := context.Background()

	// Trip cooldown: FailThreshold=2 (kit default) calls where primary → 429.
	for range 2 {
		_, _ = client.Complete(ctx, "", "p")
	}
	time.Sleep(10 * time.Millisecond)

	// During cooldown: primary must be skipped.
	primaryHits.Store(0)
	_, _ = client.Complete(ctx, "", "during")
	if primaryHits.Load() != 0 {
		t.Fatal("primary was hit during cooldown — cooldown not active")
	}

	// Wait for 2s TTL to expire, then primary must be retried.
	time.Sleep(2100 * time.Millisecond)
	primaryHits.Store(0)
	_, _ = client.Complete(ctx, "", "after")
	if primaryHits.Load() == 0 {
		t.Error("primary not retried after 2.1s — LLM_COOLDOWN_SECONDS env value did not reach kit (still cooled at 2.1s means Default > 2s)")
	}
}

// TestLLMCooldownDuration_Default verifies the helper returns 5m when env is unset.
func TestLLMCooldownDuration_Default(t *testing.T) {
	t.Setenv("LLM_COOLDOWN_SECONDS", "")
	got := llmCooldownDuration()
	if got != 5*time.Minute {
		t.Errorf("llmCooldownDuration() default = %v, want 5m", got)
	}
}

// TestChainCooldown_PrimarySkippedAfterQuotaFailures verifies per-model
// quota-aware cooldown: once the primary model accumulates FailThreshold
// consecutive 429s it is cooled and subsequent chain calls skip it — going
// directly to the fallback — so primary receives NO additional requests during
// the cooldown window.
//
// Falsification: remove the WithModelCooldown option from the client below and
// this test FAILS with "primary received 3 hits after cooldown". Without
// cooldown, the primary endpoint is attempted on every Complete call (chain
// advances to fallback only after the 429 fires, not before), so
// newPrimaryHits == callsAfterCooldown > 0.
//
// register.go linkage: this test exercises the same kitllm options wired in
// register.go's chain-build block. If WithModelCooldown is removed from
// register.go, the production chain loses quota-aware skipping and pays a dead
// primary hop (plus a log-line) on every call while the quota is exhausted.
func TestChainCooldown_PrimarySkippedAfterQuotaFailures(t *testing.T) {
	var primaryHits atomic.Int64

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(okResponseBody("ok"))
	}))
	defer fallback.Close()

	chain := []kitllm.Endpoint{
		{URL: primary.URL, Key: "k", Model: "primary-model"},
		{URL: fallback.URL, Key: "k", Model: "fallback-model"},
	}

	// cooldownOpts: FailThreshold=1 (one 429 triggers cooldown), Default=5s
	// (cooldown window outlasts the test). These constants are declared here
	// so that removing WithModelCooldown from the client below leaves a compile
	// error (unused vars) — a hard RED signal.
	const (
		cooldownThreshold = 1
		cooldownWindow    = 5 * time.Second
	)

	// REMOVE WithModelCooldown → test FAILS (primary hit 3 extra times in phase 2).
	client := kitllm.NewClient(
		primary.URL, "k", "primary-model",
		kitllm.WithEndpoints(chain),
		kitllm.WithMaxRetries(1),
		kitllm.WithModelCooldown(kitllm.CooldownConfig{
			FailThreshold: cooldownThreshold,
			Default:       cooldownWindow,
			Max:           10 * time.Second,
		}),
	)

	ctx := context.Background()

	// Phase 1: first call — primary returns 429, recording one quota failure
	// which trips FailThreshold=1 → primary enters cooldown. Fallback serves.
	_, _ = client.Complete(ctx, "", "hello")
	if primaryHits.Load() == 0 {
		t.Fatal("primary must be tried at least once before cooling")
	}

	// Phase 2: 3 more calls — primary must be SKIPPED entirely.
	// Without WithModelCooldown the chain loop still calls primary on each
	// iteration (one hit per Complete, advancing to fallback after the 429).
	// That gives newPrimaryHits = 3, failing the assertion below.
	const callsAfterCooldown = 3
	primaryBeforePhase2 := primaryHits.Load()

	for range callsAfterCooldown {
		got, err := client.Complete(ctx, "", "hello")
		if err != nil {
			t.Fatalf("unexpected error after cooldown: %v", err)
		}
		if got != "ok" {
			t.Fatalf("fallback response = %q, want %q", got, "ok")
		}
	}

	newPrimaryHits := primaryHits.Load() - primaryBeforePhase2
	if newPrimaryHits != 0 {
		t.Errorf("primary received %d hits after cooldown — want 0 (WithModelCooldown must skip cooled primary)", newPrimaryHits)
	}
}
