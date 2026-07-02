package polyglot

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

// frameworkManifestNames maps framework-specific config filenames to their
// framework language. These take precedence over generic manifestNames entries
// when both are present in the same directory.
var frameworkManifestNames = map[string]string{
	"svelte.config.js": "svelte",
	"svelte.config.ts": "svelte",
	"astro.config.mjs": "astro",
	"astro.config.ts":  "astro",
	"astro.config.js":  "astro",
}

// manifestNames maps exact manifest filenames to their language.
var manifestNames = map[string]string{
	"go.mod":            "go",
	"go.sum":            "go",
	"package.json":      "typescript",
	"package-lock.json": "typescript",
	"yarn.lock":         "typescript",
	"tsconfig.json":     "typescript",
	"Cargo.toml":        "rust",
	"Cargo.lock":        "rust",
	"pyproject.toml":    "python",
	"requirements.txt":  "python",
	"setup.py":          "python",
	"Pipfile":           "python",
	"pom.xml":           "java",
	"build.gradle":      "java",
	"build.gradle.kts":  "java",
	"Gemfile":           "ruby",
	"Gemfile.lock":      "ruby",

	// Docker manifests: marked as known but language is empty.
	// Glob-pattern variants (Dockerfile.*, *.Dockerfile, docker-compose.*.yml)
	// are matched by internal/polyglot/pinned.Collect, which keeps
	// manifestNames a simple exact-string map.
	"Dockerfile":         "",
	"docker-compose.yml": "",
	"compose.yml":        "",
}

// manifestExtensions maps file extensions to their language for manifests
// that cannot be matched by exact name (e.g. *.csproj, *.sln).
var manifestExtensions = map[string]string{
	".csproj": "csharp",
	".sln":    "csharp",
}

// findManifests returns all manifest files found among the given files.
func findManifests(files []*ingest.File) []Manifest {
	var result []Manifest

	for _, f := range files {
		name := filepath.Base(f.RelPath)
		lang := manifestLanguage(name)
		if lang != "" {
			result = append(result, Manifest{
				Path:     f.RelPath,
				Type:     name,
				Language: lang,
			})
		}
	}

	return result
}

// manifestLanguage returns the language associated with the given manifest
// filename, or "" if it is not a recognized manifest.
// Framework manifests (svelte.config.*, astro.config.*) take precedence over
// generic language manifests (package.json → "typescript").
func manifestLanguage(filename string) string {
	if lang, ok := frameworkManifestNames[filename]; ok {
		return lang
	}

	if lang, ok := manifestNames[filename]; ok {
		return lang
	}

	ext := filepath.Ext(filename)
	if lang, ok := manifestExtensions[ext]; ok {
		return lang
	}

	return ""
}

// layersFromManifests groups files by their nearest manifest directory and
// creates a layer for each group. Only primary manifests (go.mod, package.json,
// Cargo.toml, pyproject.toml, requirements.txt, pom.xml, build.gradle,
// build.gradle.kts, *.csproj, Gemfile) create layers — lock files and
// secondary manifests are included but don't define new layers.
func layersFromManifests(manifests []Manifest, files []*ingest.File) []Layer {
	// Determine which manifests are primary (define a layer root).
	primaryManifests := filterPrimaryManifests(manifests)
	if len(primaryManifests) == 0 {
		return nil
	}

	// Deduplicate by directory — keep only one manifest per directory.
	rootManifests := deduplicateByDir(primaryManifests)

	// Build layers by assigning source files to their nearest manifest root.
	type layerAccum struct {
		manifest Manifest
		langs    map[string]int
		files    int
	}

	accums := make([]layerAccum, len(rootManifests))
	for i, m := range rootManifests {
		accums[i] = layerAccum{
			manifest: m,
			langs:    make(map[string]int),
		}
	}

	for _, f := range files {
		if f.Language == "" {
			continue
		}

		idx := nearestManifest(f.RelPath, rootManifests)
		if idx < 0 {
			continue
		}

		accums[idx].langs[f.Language]++
		accums[idx].files++
	}

	var layers []Layer
	for _, a := range accums {
		if a.files == 0 {
			continue
		}

		rootDir := filepath.Dir(a.manifest.Path)
		name := filepath.Base(rootDir)
		if rootDir == "." {
			name = "root"
		}

		layers = append(layers, Layer{
			Name:     name,
			Role:     "",
			RootDir:  rootDir,
			Language: DominantLanguageFromCounts(a.langs),
			Files:    a.files,
		})
	}

	return layers
}

// primaryManifestTypes lists the manifest filenames that can create a new layer.
var primaryManifestTypes = map[string]bool{
	"go.mod":           true,
	"package.json":     true,
	"Cargo.toml":       true,
	"pyproject.toml":   true,
	"requirements.txt": true,
	"pom.xml":          true,
	"build.gradle":     true,
	"build.gradle.kts": true,
	"Gemfile":          true,
}

// filterPrimaryManifests returns only manifests that should define layers.
func filterPrimaryManifests(manifests []Manifest) []Manifest {
	var result []Manifest

	for _, m := range manifests {
		if primaryManifestTypes[m.Type] {
			result = append(result, m)
			continue
		}
		// Check extension-based manifests (*.csproj).
		ext := filepath.Ext(m.Type)
		if ext == ".csproj" {
			result = append(result, m)
		}
	}

	return result
}

// deduplicateByDir keeps only the first manifest per directory.
func deduplicateByDir(manifests []Manifest) []Manifest {
	seen := make(map[string]bool)
	var result []Manifest

	for _, m := range manifests {
		dir := filepath.Dir(m.Path)
		if !seen[dir] {
			seen[dir] = true
			result = append(result, m)
		}
	}

	return result
}

// nearestManifest returns the index of the manifest whose directory is the
// longest prefix of the given file path, or -1 if no manifest matches.
func nearestManifest(relPath string, manifests []Manifest) int {
	bestIdx := -1
	bestLen := -1

	for i, m := range manifests {
		dir := filepath.Dir(m.Path)

		// Root directory matches everything.
		if dir == "." {
			if bestLen < 0 {
				bestIdx = i
				bestLen = 0
			}
			continue
		}

		// Check if the file is under this manifest's directory.
		if strings.HasPrefix(relPath, dir+"/") || relPath == dir {
			if len(dir) > bestLen {
				bestIdx = i
				bestLen = len(dir)
			}
		}
	}

	return bestIdx
}
