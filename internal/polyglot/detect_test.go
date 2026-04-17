package polyglot

import (
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func makeFile(relPath, lang string) *ingest.File {
	return &ingest.File{
		Path:     "/repo/" + relPath,
		RelPath:  relPath,
		Language: lang,
		Size:     100,
		ModTime:  time.Now(),
	}
}

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

func TestDetectProjectLanguage_Svelte(t *testing.T) {
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

func TestDetectProjectLanguage_Astro(t *testing.T) {
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

func TestDetectProjectLanguage_FrameworkPrecedence(t *testing.T) {
	t.Parallel()

	// package.json alone → typescript; with svelte.config.ts → svelte wins.
	files := []*ingest.File{
		makeFile("package.json", ""),
		makeFile("svelte.config.ts", ""),
		makeFile("src/App.svelte", "svelte"),
	}

	manifests := findManifests(files)

	// Both manifests should be detected; svelte.config.ts must be one of them.
	langSet := make(map[string]bool)
	for _, m := range manifests {
		langSet[m.Language] = true
	}
	if !langSet["svelte"] {
		t.Errorf("expected svelte manifest in results; got manifests: %v", manifests)
	}
	// Verify that svelte.config.ts is specifically detected as "svelte", not "typescript".
	for _, m := range manifests {
		if m.Type == "svelte.config.ts" && m.Language != "svelte" {
			t.Errorf("svelte.config.ts detected as %q, want %q", m.Language, "svelte")
		}
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
