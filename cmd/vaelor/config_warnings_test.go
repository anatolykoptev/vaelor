package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// captureSlog swaps slog.Default() with a logger backed by testHandler so
// tests can assert on emitted records. Returns the captured records and a
// cleanup that restores the original default logger.
//
// Tests using this helper MUST NOT call t.Parallel() (global slog.Default).
func captureSlog(t *testing.T) (*testHandler, func()) {
	t.Helper()
	th := &testHandler{}
	orig := slog.Default()
	slog.SetDefault(slog.New(th))
	return th, func() { slog.SetDefault(orig) }
}

// warnContains searches captured records for a Warn-level record whose
// Message contains substr. Returns true if found.
func warnContains(records []slog.Record, substr string) bool {
	for _, r := range records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}

// warnContainsAttr searches captured Warn records for one whose Message
// contains msgSubstr AND has an attr with the given key whose value
// contains valSubstr.
func warnContainsAttr(records []slog.Record, msgSubstr, attrKey, valSubstr string) bool {
	for _, r := range records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if !strings.Contains(r.Message, msgSubstr) {
			continue
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == attrKey {
				if strings.Contains(a.Value.String(), valSubstr) {
					found = true
					return false
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// anyWarn reports whether any Warn-level record was captured.
func anyWarn(records []slog.Record) bool {
	for _, r := range records {
		if r.Level == slog.LevelWarn {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// #599 — KEYWORD_ARM invalid value warns and falls back to grep
// ---------------------------------------------------------------------------

func TestParseKeywordArm_InvalidValueWarns(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		wantWarn bool
		wantArm  string
	}{
		{"grep_valid", "grep", false, "grep"},
		{"bm25f_valid", "bm25f", false, "bm25f"},
		{"typo", "bm25", true, "grep"},
		{"wrong_case", "GREP", true, "grep"},
		{"empty_string", "", true, "grep"},
		{"garbage", "xyz", true, "grep"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, restore := captureSlog(t)
			defer restore()

			got := parseKeywordArm(tc.value)
			if got != tc.wantArm {
				t.Errorf("parseKeywordArm(%q) = %q, want %q", tc.value, got, tc.wantArm)
			}
			hasWarn := warnContainsAttr(th.records, "keyword arm", "env_var", "KEYWORD_ARM")
			if hasWarn != tc.wantWarn {
				t.Errorf("parseKeywordArm(%q): KEYWORD_ARM warn emitted=%v, want %v",
					tc.value, hasWarn, tc.wantWarn)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #600 — EMBED_URL unset warns that semantic_search is disabled
// ---------------------------------------------------------------------------

func TestNewSemanticDeps_EmbedURLUnsetWarns(t *testing.T) {
	th, restore := captureSlog(t)
	defer restore()

	cfg := Config{EmbedURL: ""} // EMBED_URL not set
	deps := newSemanticDeps(cfg, analyze.Deps{}, nil, nil, nil, embeddings.RRFWeights{})

	if deps.Client != nil {
		t.Error("expected nil Client when EMBED_URL unset")
	}
	if !warnContainsAttr(th.records, "semantic_search", "env_var", "EMBED_URL") {
		t.Error("expected WARN about EMBED_URL unset when EmbedURL is empty, got none")
	}
}

func TestNewSemanticDeps_EmbedURLSetNoWarn(t *testing.T) {
	th, restore := captureSlog(t)
	defer restore()

	// EmbedURL is set but dataPool is nil — semantic_search is still disabled
	// (no DB), but the EMBED_URL warning must NOT fire (it's properly set).
	cfg := Config{EmbedURL: "http://embed:8082"}
	deps := newSemanticDeps(cfg, analyze.Deps{}, nil, nil, nil, embeddings.RRFWeights{})

	if deps.Client != nil {
		t.Error("expected nil Client when dataPool is nil")
	}
	if warnContainsAttr(th.records, "semantic_search", "env_var", "EMBED_URL") {
		t.Error("expected NO EMBED_URL warning when EmbedURL is set, got one")
	}
}

// ---------------------------------------------------------------------------
// #601 — LEARNINGS_DATABASE_URL unset warns that learnings store is disabled
// ---------------------------------------------------------------------------

func TestBuildLearningsStore_DSNUnsetWarns(t *testing.T) {
	th, restore := captureSlog(t)
	defer restore()

	cfg := Config{LearningsDSN: ""}
	store := buildLearningsStore(cfg)

	if store != nil {
		t.Error("expected nil store when LearningsDSN is empty")
	}
	if !warnContainsAttr(th.records, "learnings", "env_var", "LEARNINGS_DATABASE_URL") {
		t.Error("expected WARN about LEARNINGS_DATABASE_URL unset, got none")
	}
}

func TestBuildLearningsStore_DSNSetNoConfigWarn(t *testing.T) {
	// When DSN is set (even if invalid), the "not set" warning must NOT fire.
	// A different warning ("learnings store disabled") may fire on connect
	// failure — that's expected and not what we're testing here.
	th, restore := captureSlog(t)
	defer restore()

	cfg := Config{LearningsDSN: "postgres://invalid@127.0.0.1:1/nodb"}
	_ = buildLearningsStore(cfg)

	if warnContains(th.records, "not set") {
		t.Error("expected no 'not set' warning when LearningsDSN is set, got one")
	}
}

// ---------------------------------------------------------------------------
// #602 — SPARSE_EMBED_URL unset + RRF_WEIGHT_SPARSE > 0 warns
// ---------------------------------------------------------------------------

func TestWarnSparseDisabled_URLUnsetWeightPositive(t *testing.T) {
	cases := []struct {
		name      string
		sparseURL string
		rrfSparse float64
		wantWarn  bool
	}{
		{"url_empty_weight_positive", "", 0.3, true},
		{"url_empty_weight_zero", "", 0.0, false},
		{"url_set_weight_positive", "http://embed:8082", 0.3, false},
		{"url_set_weight_zero", "http://embed:8082", 0.0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, restore := captureSlog(t)
			defer restore()

			cfg := Config{SparseEmbedURL: tc.sparseURL}
			warnSparseDisabled(cfg, embeddings.RRFWeights{Sparse: tc.rrfSparse})

			hasWarn := warnContainsAttr(th.records, "sparse", "env_var", "SPARSE_EMBED_URL")
			if hasWarn != tc.wantWarn {
				t.Errorf("warnSparseDisabled: SPARSE_EMBED_URL warn emitted=%v, want %v",
					hasWarn, tc.wantWarn)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #603 — GitHub App partial config warns naming missing fields
// ---------------------------------------------------------------------------

func TestLoadGithubAppConfig_PartialConfigWarns(t *testing.T) {
	cases := []struct {
		name           string
		appID          string
		installationID string
		keyPath        string
		wantWarnSubstr string
	}{
		{
			name:           "app_id_invalid",
			appID:          "abc",
			installationID: "",
			keyPath:        "",
			wantWarnSubstr: "GITHUB_APP_ID",
		},
		{
			name:           "app_id_set_installation_missing",
			appID:          "123",
			installationID: "",
			keyPath:        "",
			wantWarnSubstr: "GITHUB_APP_INSTALLATION_ID",
		},
		{
			name:           "app_id_set_installation_invalid",
			appID:          "123",
			installationID: "abc",
			keyPath:        "",
			wantWarnSubstr: "GITHUB_APP_INSTALLATION_ID",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, restore := captureSlog(t)
			defer restore()

			t.Setenv("VAELOR_GITHUB_APP_ID", tc.appID)
			t.Setenv("VAELOR_GITHUB_APP_INSTALLATION_ID", tc.installationID)
			if tc.keyPath != "" {
				t.Setenv("VAELOR_GITHUB_APP_KEY_PATH", tc.keyPath)
			} else {
				t.Setenv("VAELOR_GITHUB_APP_KEY_PATH", "/nonexistent/key.pem")
			}

			cfg := loadGithubAppConfig()
			if cfg.IsConfigured() {
				t.Error("expected App auth NOT configured on partial config")
			}
			if !warnContains(th.records, tc.wantWarnSubstr) {
				t.Errorf("expected WARN containing %q, got records: %v",
					tc.wantWarnSubstr, th.records)
			}
		})
	}
}

func TestLoadGithubAppConfig_UnsetNoWarn(t *testing.T) {
	// When no GitHub App env vars are set at all, App auth is simply not
	// configured — no warning should be emitted (this is the expected state).
	th, restore := captureSlog(t)
	defer restore()

	t.Setenv("VAELOR_GITHUB_APP_ID", "")
	t.Setenv("VAELOR_GITHUB_APP_INSTALLATION_ID", "")
	t.Setenv("VAELOR_GITHUB_APP_KEY_PATH", "")

	cfg := loadGithubAppConfig()
	if cfg.IsConfigured() {
		t.Error("expected App auth NOT configured when all env vars unset")
	}
	if anyWarn(th.records) {
		t.Errorf("expected NO warnings when GitHub App not configured, got: %v", th.records)
	}
}
