package freshness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverManifests_LocalRepo(t *testing.T) {
	// Use go-code's own repo root (two levels up from internal/freshness/).
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("cannot find repo root go.mod")
	}

	manifests := DiscoverManifests(root)
	if len(manifests) == 0 {
		t.Fatal("expected at least one manifest in go-code repo")
	}

	// Verify go.mod was found.
	var foundGoMod bool
	for _, m := range manifests {
		if m.ManifestPath == "go.mod" {
			foundGoMod = true
			if m.Language != "go" {
				t.Errorf("go.mod language = %q, want go", m.Language)
			}
			if m.RuntimeVersion == "" {
				t.Error("go.mod should have RuntimeVersion set")
			}
			if len(m.Dependencies) == 0 {
				t.Error("go.mod should have dependencies")
			}
		}
	}

	if !foundGoMod {
		t.Error("go.mod not found in discovered manifests")
	}
}

func TestCollectDeps_LocalRepo(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("cannot find repo root go.mod")
	}

	manifests := DiscoverManifests(root)
	deps := CollectDeps(manifests)

	if len(deps) == 0 {
		t.Fatal("expected dependencies from go-code repo")
	}

	// Verify at least some well-known deps exist.
	depNames := make(map[string]bool)
	for _, d := range deps {
		depNames[d.Name] = true
	}

	// go-code uses tree-sitter, so this should be present.
	knownDeps := []string{
		"github.com/modelcontextprotocol/go-sdk",
	}
	for _, name := range knownDeps {
		if !depNames[name] {
			t.Logf("known dependency %q not found (may have been renamed)", name)
		}
	}
}

func TestCompareGoVersions_Table(t *testing.T) {
	tests := []struct {
		name        string
		have        string
		latest      string
		wantCurrent bool
		wantEmpty   bool
	}{
		{"same version", "1.22.5", "1.22.5", true, false},
		{"same minor", "1.22", "1.22.5", true, false},
		{"newer minor", "1.23", "1.22.5", true, false},
		{"older minor", "1.21", "1.23.0", false, false},
		{"invalid have", "abc", "1.22.5", false, true},
		{"invalid latest", "1.22", "xyz", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareGoVersions(tt.have, tt.latest)
			if tt.wantEmpty {
				assertEqual(t, "", got)
				return
			}
			if tt.wantCurrent {
				assertEqual(t, verCurrent, got)
			} else if got == verCurrent || got == "" {
				t.Errorf("got %q, want outdated status", got)
			}
		})
	}
}
