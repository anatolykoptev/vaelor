package polyglot

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestFindManifests(t *testing.T) {
	t.Parallel()

	files := []*ingest.File{
		makeFile("go.mod", ""),
		makeFile("frontend/package.json", ""),
		makeFile("main.go", "go"),
		makeFile("scripts/requirements.txt", ""),
		makeFile("scripts/pyproject.toml", ""),
		makeFile("README.md", ""),
	}

	manifests := findManifests(files)

	if len(manifests) != 4 {
		t.Errorf("findManifests: got %d manifests, want 4", len(manifests))
		for _, m := range manifests {
			t.Logf("  manifest: %s (%s)", m.Path, m.Language)
		}
	}
}

func TestFindManifests_Svelte(t *testing.T) {
	t.Parallel()

	files := []*ingest.File{
		makeFile("svelte.config.js", ""),
		makeFile("package.json", ""),
		makeFile("src/App.svelte", "svelte"),
		makeFile("src/lib/util.ts", "typescript"),
	}

	manifests := findManifests(files)

	var svelteFound bool
	for _, m := range manifests {
		if m.Language == "svelte" {
			svelteFound = true
			break
		}
	}
	if !svelteFound {
		t.Errorf("expected svelte manifest to be detected; got manifests: %v", manifests)
	}
}

func TestFindManifests_Astro(t *testing.T) {
	t.Parallel()

	files := []*ingest.File{
		makeFile("astro.config.mjs", ""),
		makeFile("package.json", ""),
		makeFile("src/pages/index.astro", "astro"),
	}

	manifests := findManifests(files)

	var astroFound bool
	for _, m := range manifests {
		if m.Language == "astro" {
			astroFound = true
			break
		}
	}
	if !astroFound {
		t.Errorf("expected astro manifest to be detected; got manifests: %v", manifests)
	}
}

func TestFindManifests_FrameworkAndPackageJSONCoexist(t *testing.T) {
	t.Parallel()

	// package.json alone → typescript; with svelte.config.ts → both manifests detected.
	files := []*ingest.File{
		makeFile("package.json", ""),
		makeFile("svelte.config.ts", ""),
		makeFile("src/App.svelte", "svelte"),
	}

	manifests := findManifests(files)

	langSet := make(map[string]bool)
	for _, m := range manifests {
		langSet[m.Language] = true
	}
	if !langSet["svelte"] {
		t.Errorf("expected svelte manifest in results; got manifests: %v", manifests)
	}
	// svelte.config.ts must be detected as "svelte", not "typescript".
	for _, m := range manifests {
		if m.Type == "svelte.config.ts" && m.Language != "svelte" {
			t.Errorf("svelte.config.ts detected as %q, want %q", m.Language, "svelte")
		}
	}
}

func TestManifestLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename string
		want     string
	}{
		{"go.mod", "go"},
		{"package.json", "typescript"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"Gemfile", "ruby"},
		{"foo.csproj", "csharp"},
		{"unknown.txt", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			t.Parallel()

			got := manifestLanguage(tt.filename)
			if got != tt.want {
				t.Errorf("manifestLanguage(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestManifestLanguage_FrameworkManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename string
		want     string
	}{
		{"svelte.config.js", "svelte"},
		{"svelte.config.ts", "svelte"},
		{"astro.config.mjs", "astro"},
		{"astro.config.ts", "astro"},
		{"astro.config.js", "astro"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			t.Parallel()

			got := manifestLanguage(tt.filename)
			if got != tt.want {
				t.Errorf("manifestLanguage(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}
