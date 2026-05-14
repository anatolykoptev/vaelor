package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/graphx"
)

// TestBuildGraphDeps_NoStore verifies that buildGraphDeps returns graphx.Noop{}
// for both Analytics and CrossRefs when no store is available (empty DATABASE_URL).
func TestBuildGraphDeps_NoStore(t *testing.T) {
	analytics, refs := buildGraphDeps(nil, nil)

	if analytics == nil {
		t.Fatal("Graph must be non-nil")
	}
	if refs == nil {
		t.Fatal("Refs must be non-nil")
	}

	if _, ok := analytics.(graphx.Noop); !ok {
		t.Errorf("Graph: expected graphx.Noop, got %T", analytics)
	}
	if _, ok := refs.(graphx.Noop); !ok {
		t.Errorf("Refs: expected graphx.Noop, got %T", refs)
	}
}

func TestBuildCloneTokenFunc_PATFallback(t *testing.T) {
	cfg := Config{
		GithubToken:     "gho_test_pat",
		GithubAppConfig: forge.AppConfig{}, // not configured
	}
	fn := buildCloneTokenFunc(cfg)
	if fn == nil {
		t.Fatal("buildCloneTokenFunc returned nil")
	}
	tok, err := fn(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "gho_test_pat" {
		t.Errorf("token = %q, want %q", tok, "gho_test_pat")
	}
}

func TestBuildCloneTokenFunc_AppConfigured_BadKey(t *testing.T) {
	// When App is "configured" (all fields set) but the PEM is invalid,
	// buildCloneTokenFunc must fall back to the PAT — not panic or crash.
	cfg := Config{
		GithubToken: "gho_fallback",
		GithubAppConfig: forge.AppConfig{
			AppID:          42,
			InstallationID: 99,
			KeyPEM:         []byte("not-a-valid-pem"),
		},
	}
	fn := buildCloneTokenFunc(cfg)
	if fn == nil {
		t.Fatal("buildCloneTokenFunc returned nil")
	}
	tok, err := fn(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "gho_fallback" {
		t.Errorf("token = %q, want %q", tok, "gho_fallback")
	}
}
