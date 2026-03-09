package freshness

import (
	"testing"
)

func TestParseCargoTomlFreshness_Full(t *testing.T) {
	input := `[package]
name = "my-crate"
version = "0.1.0"
edition = "2021"
rust-version = "1.75"

[dependencies]
serde = "1.0"
tokio = { version = "1.35", features = ["full"] }

[dev-dependencies]
assert_cmd = "2.0"
`
	info := ParseCargoTomlFreshness([]byte(input))

	if info.Language != "rust" {
		t.Errorf("Language = %q, want %q", info.Language, "rust")
	}
	if info.RuntimeVersion != "1.75" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, "1.75")
	}

	wantDeps := 3
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	// Verify inline table version extraction.
	for _, dep := range info.Dependencies {
		if dep.Name == "tokio" && dep.Version != "1.35" {
			t.Errorf("tokio version = %q, want %q", dep.Version, "1.35")
		}
	}
}

func TestParseCargoTomlFreshness_EditionOnly(t *testing.T) {
	input := `[package]
name = "minimal"
edition = "2021"

[dependencies]
log = "0.4"
`
	info := ParseCargoTomlFreshness([]byte(input))

	if info.RuntimeVersion != "2021" {
		t.Errorf("RuntimeVersion = %q, want %q (edition fallback)", info.RuntimeVersion, "2021")
	}
}

func TestParseCargoTomlFreshness_NoDeps(t *testing.T) {
	input := `[package]
name = "bare"
version = "0.1.0"
`
	info := ParseCargoTomlFreshness([]byte(input))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParseCargoTomlFreshness_PathDep(t *testing.T) {
	input := `[dependencies]
local-crate = { path = "../local" }
versioned = { version = "2.0", optional = true }
`
	info := ParseCargoTomlFreshness([]byte(input))
	if len(info.Dependencies) != 2 {
		t.Fatalf("Dependencies count = %d, want 2", len(info.Dependencies))
	}

	for _, dep := range info.Dependencies {
		if dep.Name == "local-crate" && dep.Version != "" {
			t.Errorf("path dep should have empty version, got %q", dep.Version)
		}
		if dep.Name == "versioned" && dep.Version != "2.0" {
			t.Errorf("versioned dep version = %q, want %q", dep.Version, "2.0")
		}
	}
}
