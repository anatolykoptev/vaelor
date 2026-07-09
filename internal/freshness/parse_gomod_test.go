package freshness

import (
	"testing"
)

func TestParseGoMod_Full(t *testing.T) {
	t.Parallel()
	input := `module github.com/example/foo

go 1.22.0

require (
	github.com/bar/baz v1.2.3
	github.com/qux/quux v0.5.0 // indirect
)

require github.com/single/dep v2.0.0
`
	info := ParseGoMod([]byte(input))

	if info.Language != "go" {
		t.Errorf("Language = %q, want %q", info.Language, "go")
	}
	if info.RuntimeVersion != "1.22.0" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, "1.22.0")
	}

	wantDeps := 3
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	// Check that indirect comment is stripped from version.
	for _, dep := range info.Dependencies {
		if dep.Name == "github.com/qux/quux" {
			if dep.Version != "v0.5.0" {
				t.Errorf("indirect dep version = %q, want %q", dep.Version, "v0.5.0")
			}
		}
	}
}

func TestParseGoMod_MultipleBlocks(t *testing.T) {
	t.Parallel()
	input := `module example.com/m

go 1.21

require (
	a.com/x v1.0.0
)

require (
	b.com/y v2.0.0
)
`
	info := ParseGoMod([]byte(input))

	if len(info.Dependencies) != 2 {
		t.Errorf("Dependencies count = %d, want 2", len(info.Dependencies))
	}
}

func TestParseGoMod_Minimal(t *testing.T) {
	t.Parallel()
	input := `module example.com/m

go 1.20
`
	info := ParseGoMod([]byte(input))
	if info.RuntimeVersion != "1.20" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, "1.20")
	}
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParseGoMod_SingleLineRequire(t *testing.T) {
	t.Parallel()
	input := `module example.com/m

go 1.21

require github.com/pkg/errors v0.9.1
`
	info := ParseGoMod([]byte(input))
	if len(info.Dependencies) != 1 {
		t.Fatalf("Dependencies count = %d, want 1", len(info.Dependencies))
	}
	if info.Dependencies[0].Name != "github.com/pkg/errors" {
		t.Errorf("dep name = %q", info.Dependencies[0].Name)
	}
}
