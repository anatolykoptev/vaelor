package polyglot

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestDetectStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      []*ingest.File
		wantLangs  int
		wantLayers int
	}{
		{
			name: "go only repo",
			files: []*ingest.File{
				makeFile("go.mod", ""),
				makeFile("main.go", "go"),
				makeFile("handler.go", "go"),
			},
			wantLangs:  1,
			wantLayers: 1,
		},
		{
			name: "go backend + ts frontend",
			files: []*ingest.File{
				makeFile("backend/go.mod", ""),
				makeFile("backend/main.go", "go"),
				makeFile("backend/handler.go", "go"),
				makeFile("backend/service.go", "go"),
				makeFile("frontend/package.json", ""),
				makeFile("frontend/src/app.ts", "typescript"),
				makeFile("frontend/src/index.ts", "typescript"),
				makeFile("frontend/src/utils.ts", "typescript"),
			},
			wantLangs:  2,
			wantLayers: 2,
		},
		{
			name: "monorepo with three languages",
			files: []*ingest.File{
				makeFile("services/api/go.mod", ""),
				makeFile("services/api/main.go", "go"),
				makeFile("services/api/handler.go", "go"),
				makeFile("web/package.json", ""),
				makeFile("web/src/app.tsx", "typescript"),
				makeFile("web/src/index.ts", "typescript"),
				makeFile("scripts/pyproject.toml", ""),
				makeFile("scripts/deploy.py", "python"),
				makeFile("scripts/migrate.py", "python"),
			},
			wantLangs:  3,
			wantLayers: 3,
		},
		{
			name: "flat repo no manifests",
			files: []*ingest.File{
				makeFile("main.go", "go"),
				makeFile("handler.go", "go"),
				makeFile("util.go", "go"),
			},
			wantLangs:  1,
			wantLayers: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rs := DetectStructure(tt.files)

			if rs == nil {
				t.Fatal("DetectStructure returned nil")
			}

			gotLangs := len(rs.Languages)
			if gotLangs != tt.wantLangs {
				t.Errorf("languages: got %d, want %d (langs=%v)", gotLangs, tt.wantLangs, rs.Languages)
			}

			gotLayers := len(rs.Layers)
			if gotLayers != tt.wantLayers {
				t.Errorf("layers: got %d, want %d", gotLayers, tt.wantLayers)
			}
		})
	}
}

func TestDetectStructure_SvelteProjectPrimaryLanguage(t *testing.T) {
	t.Parallel()

	// Fixture: svelte.config.js + package.json + 2 Svelte source files + 1 TypeScript.
	// DetectStructure must produce a layer with Language == "svelte" (dominant source language).
	files := []*ingest.File{
		makeFile("svelte.config.js", ""),
		makeFile("package.json", ""),
		makeFile("src/App.svelte", "svelte"),
		makeFile("src/Other.svelte", "svelte"),
		makeFile("src/lib/util.ts", "typescript"),
	}

	rs := DetectStructure(files)

	if rs == nil {
		t.Fatal("DetectStructure returned nil")
	}
	if len(rs.Layers) == 0 {
		t.Fatal("DetectStructure returned no layers")
	}

	// Layers[0] must be "svelte": 2 svelte source files vs 1 typescript.
	got := rs.Layers[0].Language
	if got != "svelte" {
		t.Errorf("Layers[0].Language = %q, want %q (layers=%v, languages=%v)",
			got, "svelte", rs.Layers, rs.Languages)
	}
}

func TestDetectStructure_AstroProjectPrimaryLanguage(t *testing.T) {
	t.Parallel()

	// Fixture: astro.config.mjs + package.json + 2 Astro source files + 1 TypeScript.
	// DetectStructure must produce a layer with Language == "astro" (dominant source language).
	files := []*ingest.File{
		makeFile("astro.config.mjs", ""),
		makeFile("package.json", ""),
		makeFile("src/pages/Index.astro", "astro"),
		makeFile("src/pages/About.astro", "astro"),
		makeFile("src/util.ts", "typescript"),
	}

	rs := DetectStructure(files)

	if rs == nil {
		t.Fatal("DetectStructure returned nil")
	}
	if len(rs.Layers) == 0 {
		t.Fatal("DetectStructure returned no layers")
	}

	// Layers[0] must be "astro": 2 astro source files vs 1 typescript.
	got := rs.Layers[0].Language
	if got != "astro" {
		t.Errorf("Layers[0].Language = %q, want %q (layers=%v, languages=%v)",
			got, "astro", rs.Layers, rs.Languages)
	}
}

func TestIsPolyglot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rs   *RepoStructure
		want bool
	}{
		{
			name: "single language",
			rs: &RepoStructure{
				Languages: map[string]int{"go": 10},
			},
			want: false,
		},
		{
			name: "two languages under threshold",
			rs: &RepoStructure{
				Languages: map[string]int{"go": 4, "python": 3},
			},
			want: false,
		},
		{
			name: "two languages above threshold",
			rs: &RepoStructure{
				Languages: map[string]int{"go": 10, "typescript": 5},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.rs.IsPolyglot()
			if got != tt.want {
				t.Errorf("IsPolyglot() = %v, want %v", got, tt.want)
			}
		})
	}
}
