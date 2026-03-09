package freshness

import (
	"context"
	"fmt"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		current, latest, want string
	}{
		// Exact match.
		{"1.2.3", "1.2.3", "current"},
		{"v1.2.3", "v1.2.3", "current"},
		// Go v-prefix vs no prefix.
		{"v1.2.3", "1.2.3", "current"},
		{"1.2.3", "v1.2.3", "current"},
		// Minor outdated.
		{"1.0.0", "1.2.0", "minor"},
		{"1.2.3", "1.3.0", "minor"},
		{"v1.0.0", "v1.0.1", "minor"},
		// Major outdated.
		{"1.2.3", "2.0.0", "major"},
		{"v0.5.0", "v1.0.0", "major"},
		// NPM prefix operators — strip before compare.
		{"^1.2.3", "1.2.3", "current"},
		{"~1.0.0", "1.2.0", "minor"},
		{">=1.0.0", "2.0.0", "major"},
		// Partial versions.
		{"1.0", "1.0.0", "current"},
		{"1.0", "1.1.0", "minor"},
		{"1", "2.0.0", "major"},
		// Unparseable.
		{"latest", "1.0.0", "unknown"},
		{"", "1.0.0", "unknown"},
		{"1.0.0", "", "unknown"},
		{"abc", "def", "unknown"},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%s_vs_%s", tt.current, tt.latest)
		t.Run(name, func(t *testing.T) {
			got := compareSemver(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %q, want %q",
					tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// mockRegistry implements Registry for testing.
type mockRegistry struct {
	versions map[string]string
}

func (m *mockRegistry) Latest(_ context.Context, name string) (string, error) {
	v, ok := m.versions[name]
	if !ok {
		return "", fmt.Errorf("not found: %s", name)
	}
	return v, nil
}

func TestCheckFreshness_AllCurrent(t *testing.T) {
	deps := []Dependency{
		{Name: "foo", Version: "1.0.0", Language: "go"},
		{Name: "bar", Version: "2.3.4", Language: "go"},
	}
	reg := &MultiRegistry{registries: map[string]Registry{
		"go": &mockRegistry{versions: map[string]string{
			"foo": "1.0.0",
			"bar": "2.3.4",
		}},
	}}

	result := CheckFreshness(context.Background(), deps, reg)
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if result.UpToDate != 2 {
		t.Errorf("UpToDate = %d, want 2", result.UpToDate)
	}
	if result.Ratio != 1.0 {
		t.Errorf("Ratio = %f, want 1.0", result.Ratio)
	}
	if len(result.Outdated) != 0 {
		t.Errorf("Outdated len = %d, want 0", len(result.Outdated))
	}
}

func TestCheckFreshness_Mixed(t *testing.T) {
	deps := []Dependency{
		{Name: "a", Version: "1.0.0", Language: "npm"},
		{Name: "b", Version: "1.0.0", Language: "npm"},
		{Name: "c", Version: "1.0.0", Language: "npm"},
	}
	reg := &MultiRegistry{registries: map[string]Registry{
		"npm": &mockRegistry{versions: map[string]string{
			"a": "1.0.0", // current
			"b": "1.2.0", // minor
			"c": "2.0.0", // major
		}},
	}}

	result := CheckFreshness(context.Background(), deps, reg)
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if result.UpToDate != 1 {
		t.Errorf("UpToDate = %d, want 1", result.UpToDate)
	}
	if result.MinorOutdated != 1 {
		t.Errorf("MinorOutdated = %d, want 1", result.MinorOutdated)
	}
	if result.MajorOutdated != 1 {
		t.Errorf("MajorOutdated = %d, want 1", result.MajorOutdated)
	}
	if len(result.Outdated) != 2 {
		t.Errorf("Outdated len = %d, want 2", len(result.Outdated))
	}
}

func TestCheckFreshness_Empty(t *testing.T) {
	reg := &MultiRegistry{registries: map[string]Registry{}}
	result := CheckFreshness(context.Background(), nil, reg)
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if result.Ratio != 1.0 {
		t.Errorf("Ratio = %f, want 1.0 for empty deps", result.Ratio)
	}
}

func TestCheckFreshness_RegistryError(t *testing.T) {
	deps := []Dependency{
		{Name: "ok", Version: "1.0.0", Language: "go"},
		{Name: "fail", Version: "1.0.0", Language: "go"},
	}
	reg := &MultiRegistry{registries: map[string]Registry{
		"go": &mockRegistry{versions: map[string]string{
			"ok": "1.0.0",
			// "fail" missing → error
		}},
	}}

	result := CheckFreshness(context.Background(), deps, reg)
	// Should still count "ok" but skip "fail".
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1 (skipped errored dep)", result.Total)
	}
}

func TestCheckFreshness_UnknownLanguage(t *testing.T) {
	deps := []Dependency{
		{Name: "x", Version: "1.0.0", Language: "haskell"},
	}
	reg := &MultiRegistry{registries: map[string]Registry{}}

	result := CheckFreshness(context.Background(), deps, reg)
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0 (unknown language skipped)", result.Total)
	}
}

func TestCheckFreshness_MultiLanguage(t *testing.T) {
	deps := []Dependency{
		{Name: "a", Version: "1.0.0", Language: "go"},
		{Name: "b", Version: "1.0.0", Language: "npm"},
	}
	reg := &MultiRegistry{registries: map[string]Registry{
		"go":  &mockRegistry{versions: map[string]string{"a": "1.0.0"}},
		"npm": &mockRegistry{versions: map[string]string{"b": "2.0.0"}},
	}}

	result := CheckFreshness(context.Background(), deps, reg)
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if result.UpToDate != 1 {
		t.Errorf("UpToDate = %d, want 1", result.UpToDate)
	}
	if result.MajorOutdated != 1 {
		t.Errorf("MajorOutdated = %d, want 1", result.MajorOutdated)
	}
}
