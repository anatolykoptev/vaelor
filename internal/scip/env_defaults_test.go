package scip

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIndexerEnv_StableCachesSurviveThrowawayHome(t *testing.T) {
	realHome := t.TempDir() // no .cargo yet: fresh container, cargo creates it
	none := func(string) string { return "" }

	rust := indexerEnv(indexerRustAnalyzer, "/tmp/h", realHome, none)
	if !slices.Contains(rust, "CARGO_HOME="+filepath.Join(realHome, ".cargo")) {
		t.Errorf("rust-analyzer env lacks stable CARGO_HOME: %v", rust)
	}
	if !slices.Contains(rust, "HOME=/tmp/h") {
		t.Errorf("HOME not the throwaway dir: %v", rust)
	}

	java := indexerEnv(indexerScipJava, "/tmp/h", realHome, none)
	if !slices.Contains(java, "COURSIER_CACHE="+filepath.Join(realHome, ".cache", "coursier")) {
		t.Errorf("scip-java env lacks stable COURSIER_CACHE: %v", java)
	}

	if !slices.Contains(rust, "GIT_CONFIG_KEY_0=safe.directory") {
		t.Errorf("rust-analyzer env lacks safe.directory override: %v", rust)
	}

	// An explicit value from the parent wins and is not duplicated.
	set := func(k string) string {
		if k == "CARGO_HOME" {
			return "/custom/cargo"
		}
		return ""
	}
	got := indexerEnv(indexerRustAnalyzer, "/tmp/h", realHome, set)
	n := 0
	for _, e := range got {
		if len(e) > 11 && e[:11] == "CARGO_HOME=" {
			n++
			if e != "CARGO_HOME=/custom/cargo" {
				t.Errorf("CARGO_HOME = %q, want parent value", e)
			}
		}
	}
	if n != 1 {
		t.Errorf("CARGO_HOME appears %d times, want 1: %v", n, got)
	}

	// Other indexers get no cargo/coursier locations.
	ts := indexerEnv("scip-typescript", "/tmp/h", realHome, none)
	for _, e := range ts {
		if e != "HOME=/tmp/h" {
			t.Errorf("scip-typescript with empty parent env should only have HOME, got %v", ts)
		}
	}
}

func TestTrustedRootsExcluding(t *testing.T) {
	base := t.TempDir()
	trusted := filepath.Join(base, "src")
	clean := filepath.Join(base, "other")
	for _, d := range []string{trusted, clean} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(trusted, "ws") // workspace nested inside a trusted dir
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}

	got := TrustedRootsExcluding([]string{trusted, clean}, nested)
	if !slices.Equal(got, []string{clean}) {
		t.Errorf("TrustedRootsExcluding = %v, want only %v", got, clean)
	}
	// Reverse overlap: the trusted dir sits INSIDE the untrusted location.
	if got := TrustedRootsExcluding([]string{nested}, trusted); len(got) != 0 {
		t.Errorf("trusted dir nested inside an untrusted one was kept: %v", got)
	}
	// Disjoint workspace: nothing dropped.
	if got := TrustedRootsExcluding([]string{trusted, clean}, t.TempDir()); len(got) != 2 {
		t.Errorf("disjoint workspace dropped dirs: %v", got)
	}
}
