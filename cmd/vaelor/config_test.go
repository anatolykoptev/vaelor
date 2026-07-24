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

// TestInertRankWeightEnvVars_MinmaxMode proves the AnalyzeRankWeight* knobs
// are explicitly flagged as inert when set in the default minmax mode: the
// weights only apply to the rrf path (rank_fusion.go), so an operator setting
// them in minmax mode configures a silent no-op. This makes that explicit
// instead of advertising the knob as global.
//
// Falsification: revert inertRankWeightEnvVars to always return nil → the
// minmax+set case returns no inert vars → the assertion goes RED.
func TestInertRankWeightEnvVars_MinmaxMode(t *testing.T) {
	// minmax + explicitly set weights → inert (flagged).
	t.Setenv("ANALYZE_RANK_FUSION_MODE", "")
	t.Setenv("ANALYZE_RANK_WEIGHT_BM25", "2.0")
	t.Setenv("ANALYZE_RANK_WEIGHT_PAGERANK", "3.0")
	inert := inertRankWeightEnvVars(analyze.FusionModeMinmax)
	if len(inert) != 2 {
		t.Fatalf("minmax+set: got %d inert vars, want 2 (%v)", len(inert), inert)
	}

	// rrf mode → weights are honored, none inert.
	inert = inertRankWeightEnvVars(analyze.FusionModeRRF)
	if inert != nil {
		t.Errorf("rrf mode: got %v inert vars, want nil (weights honored in rrf)", inert)
	}

	// minmax + no weights set → nothing to flag.
	t.Setenv("ANALYZE_RANK_WEIGHT_BM25", "")
	t.Setenv("ANALYZE_RANK_WEIGHT_PAGERANK", "")
	inert = inertRankWeightEnvVars(analyze.FusionModeMinmax)
	if inert != nil {
		t.Errorf("minmax+unset: got %v inert vars, want nil", inert)
	}
}

// ---------------------------------------------------------------------------
// #660/#661 — Phase E: default keyword arm to bm25f at RRF weight 0.5
// ---------------------------------------------------------------------------
//
// Empirical 4-repo golden eval (194 queries, python/ts/java/rust) picked
// bm25f @ keyword-weight 0.5 as the new default (OVERALL nDCG@10 0.568 vs
// 0.499 for the prior grep@1.0 default; +0.069, no per-language regression).
// A fresh deploy (no env overrides) must resolve to this config. The env
// overrides (KEYWORD_ARM, RRF_WEIGHT_KEYWORD) must still win.
//
// Falsification (per-case, revert the specific default → RED):
//   - TestLoadConfig_DefaultKeywordArmBm25f: revert defaultKeywordArm to
//     keywordArmGrep → KeywordArm=="grep" → RED.
//   - TestLoadConfig_DefaultRRFWeightKeyword05: revert defaultRRFWeightKeyword
//     to 1.0 → RRFWeightKeyword==1.0 → RED.
//   - TestLoadConfig_KeywordArmGrepOverride / RRFWeightKeywordOverride:
//     these stay GREEN regardless (they assert the override path, not the
//     default), proving the override plumbing is untouched.

// TestLoadConfig_DefaultKeywordArmBm25f confirms a fresh deploy (KEYWORD_ARM
// unset) resolves the keyword arm to bm25f — the Phase E promoted default.
func TestLoadConfig_DefaultKeywordArmBm25f(t *testing.T) {
	t.Setenv("KEYWORD_ARM", "") // unset → default
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.KeywordArm != keywordArmBM25F {
		t.Errorf("default KeywordArm = %q, want %q (bm25f is the Phase E promoted default)",
			cfg.KeywordArm, keywordArmBM25F)
	}
}

// TestLoadConfig_DefaultRRFWeightKeyword05 confirms a fresh deploy
// (RRF_WEIGHT_KEYWORD unset) resolves the keyword-arm RRF weight to 0.5 —
// the inverted-U peak from the empirical eval.
func TestLoadConfig_DefaultRRFWeightKeyword05(t *testing.T) {
	t.Setenv("RRF_WEIGHT_KEYWORD", "") // unset → default
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.RRFWeightKeyword != 0.5 {
		t.Errorf("default RRFWeightKeyword = %g, want 0.5 (Phase E promoted default)",
			cfg.RRFWeightKeyword)
	}
}

// TestLoadConfig_KeywordArmGrepOverride confirms KEYWORD_ARM=grep still
// selects the grep arm — the env override must win over the new default.
func TestLoadConfig_KeywordArmGrepOverride(t *testing.T) {
	t.Setenv("KEYWORD_ARM", "grep")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.KeywordArm != keywordArmGrep {
		t.Errorf("KEYWORD_ARM=grep: KeywordArm = %q, want %q (override must win)",
			cfg.KeywordArm, keywordArmGrep)
	}
}

// TestLoadConfig_RRFWeightKeywordOverride confirms RRF_WEIGHT_KEYWORD=0.9
// still sets the keyword weight to 0.9 — the env override must win over the
// new 0.5 default.
func TestLoadConfig_RRFWeightKeywordOverride(t *testing.T) {
	t.Setenv("RRF_WEIGHT_KEYWORD", "0.9")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.RRFWeightKeyword != 0.9 {
		t.Errorf("RRF_WEIGHT_KEYWORD=0.9: RRFWeightKeyword = %g, want 0.9 (override must win)",
			cfg.RRFWeightKeyword)
	}
}
