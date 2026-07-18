package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
)

// TestLoadConfig_InvalidFusionMode rejects ANALYZE_RANK_FUSION_MODE values
// outside {minmax, rrf}. Typos must surface loudly so operators don't silently
// fall back to the default and wonder why the rrf path never activates.
func TestLoadConfig_InvalidFusionMode(t *testing.T) {
	t.Setenv("ANALYZE_RANK_FUSION_MODE", "rrrf") // typo
	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig: want error on invalid fusion mode, got nil")
	}
	if !strings.Contains(err.Error(), "ANALYZE_RANK_FUSION_MODE") {
		t.Errorf("loadConfig error = %v; want mention of ANALYZE_RANK_FUSION_MODE", err)
	}
}

// TestLoadConfig_DefaultFusionMode confirms the default is byte-identical
// legacy minmax — Stream 3 must NOT flip the default in this PR.
func TestLoadConfig_DefaultFusionMode(t *testing.T) {
	t.Setenv("ANALYZE_RANK_FUSION_MODE", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.AnalyzeRankFusionMode != analyze.FusionModeMinmax {
		t.Errorf("default fusion mode = %q, want %q", cfg.AnalyzeRankFusionMode, analyze.FusionModeMinmax)
	}
}

// TestLoadConfig_RRFModeAccepted is the minimal positive case for the opt-in
// path the offline harness will exercise.
func TestLoadConfig_RRFModeAccepted(t *testing.T) {
	t.Setenv("ANALYZE_RANK_FUSION_MODE", "rrf")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.AnalyzeRankFusionMode != analyze.FusionModeRRF {
		t.Errorf("fusion mode = %q, want rrf", cfg.AnalyzeRankFusionMode)
	}
}

// TestLoadConfig_NegativeWeightRejected guards the WeightedRRF panic invariant
// at config-parse time so a misconfigured weight never reaches rerank.
func TestLoadConfig_NegativeWeightRejected(t *testing.T) {
	t.Setenv("ANALYZE_RANK_WEIGHT_BM25", "-0.5")
	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig: want error on negative weight, got nil")
	}
	if !strings.Contains(err.Error(), "ANALYZE_RANK_WEIGHT_BM25") {
		t.Errorf("loadConfig error = %v; want mention of ANALYZE_RANK_WEIGHT_BM25", err)
	}
}

// TestLoadConfig_SparseBackfillDeadline_ZeroClamped guards the footgun where
// SPARSE_BACKFILL_DEADLINE_S=0 would produce a 0-duration deadline, causing the
// MCP harness to fall back to its 90s global default — silently re-introducing
// the truncation this field was added to fix (103K-row backfill > 90s).
//
// Falsification: revert clampSparseBackfillDeadline to the bare multiplication
// `time.Duration(secs) * time.Second` → zero input → 0s deadline stored →
// cfg.SparseBackfillDeadline == 0 ≠ defaultDeadline, test goes RED.
// TestLoadConfig_LLMPerAttemptTimeoutDefault verifies the per-attempt cap is
// enabled by default (non-zero) so the go-kit model chain rotates past a slow
// endpoint instead of a single slow attempt consuming the whole tool deadline
// (the code_compare empty-recommendation failure mode).
func TestLoadConfig_LLMPerAttemptTimeoutDefault(t *testing.T) {
	// Force the unset path hermetically: env.Duration treats "" as unset and
	// returns the default, so this holds even if the runner exports the var.
	t.Setenv("LLM_PER_ATTEMPT_TIMEOUT", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.LLMPerAttemptTimeout != defaultLLMPerAttemptTimeout {
		t.Errorf("LLMPerAttemptTimeout default: want %s, got %s", defaultLLMPerAttemptTimeout, cfg.LLMPerAttemptTimeout)
	}

	// Explicit override must pass through.
	t.Setenv("LLM_PER_ATTEMPT_TIMEOUT", "45s")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.LLMPerAttemptTimeout != 45*time.Second {
		t.Errorf("LLM_PER_ATTEMPT_TIMEOUT=45s: want 45s, got %s", cfg.LLMPerAttemptTimeout)
	}
}

// TestLoadConfig_LLMModelDefault guards the fresh-deploy fallback model id.
// Prod always overrides via LLM_MODEL (fleet llm.env), so this only fires on
// a fresh deploy without that env — it must stay a currently-live
// cliproxyapi model, not a removed one (a dead default previously silently
// 502'd "unknown provider").
func TestLoadConfig_LLMModelDefault(t *testing.T) {
	t.Setenv("LLM_MODEL", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.LLMModel != defaultLLMModel {
		t.Errorf("LLMModel default = %q, want %q", cfg.LLMModel, defaultLLMModel)
	}

	// Explicit override must pass through.
	t.Setenv("LLM_MODEL", "some-other-model")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.LLMModel != "some-other-model" {
		t.Errorf("LLM_MODEL override: want %q, got %q", "some-other-model", cfg.LLMModel)
	}
}

// TestLoadConfig_SourcemapRateLimitDefault guards the /resolve per-IP rate
// limiter being enabled by default (issue #388: it used to ship disabled,
// leaving a network-exposed, bearer-auth-gated endpoint unthrottled).
func TestLoadConfig_SourcemapRateLimitDefault(t *testing.T) {
	// Force the unset path hermetically: env.Float/env.Int treat "" as unset
	// and return the default, so this holds even if the runner exports the
	// var.
	t.Setenv("SOURCEMAP_RATE_LIMIT", "")
	t.Setenv("SOURCEMAP_RATE_BURST", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SourcemapRateLimit != 30 {
		t.Errorf("SourcemapRateLimit default = %v, want 30", cfg.SourcemapRateLimit)
	}
	if cfg.SourcemapRateBurst != 60 {
		t.Errorf("SourcemapRateBurst default = %v, want 60", cfg.SourcemapRateBurst)
	}

	// Explicit override must pass through (including the operator opt-out
	// path, SOURCEMAP_RATE_LIMIT=0).
	t.Setenv("SOURCEMAP_RATE_LIMIT", "5")
	t.Setenv("SOURCEMAP_RATE_BURST", "15")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SourcemapRateLimit != 5 {
		t.Errorf("SOURCEMAP_RATE_LIMIT=5: want 5, got %v", cfg.SourcemapRateLimit)
	}
	if cfg.SourcemapRateBurst != 15 {
		t.Errorf("SOURCEMAP_RATE_BURST=15: want 15, got %v", cfg.SourcemapRateBurst)
	}
}

func TestLoadConfig_SparseBackfillDeadline_ZeroClamped(t *testing.T) {
	const defaultDeadline = defaultSparseBackfillDeadlineS * time.Second

	for _, input := range []string{"0", "-1", "-600"} {
		t.Run("env="+input, func(t *testing.T) {
			t.Setenv("SPARSE_BACKFILL_DEADLINE_S", input)
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.SparseBackfillDeadline != defaultDeadline {
				t.Errorf("SPARSE_BACKFILL_DEADLINE_S=%s: want %s (clamped to default), got %s",
					input, defaultDeadline, cfg.SparseBackfillDeadline)
			}
		})
	}

	// Positive value must pass through unchanged.
	t.Run("env=120", func(t *testing.T) {
		t.Setenv("SPARSE_BACKFILL_DEADLINE_S", "120")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if cfg.SparseBackfillDeadline != 120*time.Second {
			t.Errorf("SPARSE_BACKFILL_DEADLINE_S=120: want 120s, got %s", cfg.SparseBackfillDeadline)
		}
	})
}
