// Package polyglot detects multi-language repository structures by scanning
// manifest files (go.mod, package.json, Cargo.toml, etc.) and grouping
// source files into language-specific layers.
package polyglot

import (
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// RepoStructure describes the detected language layout of a repository.
type RepoStructure struct {
	Layers    []Layer
	Languages map[string]int
	Manifests []Manifest
}

// Layer represents a distinct language area within the repository, typically
// rooted at a manifest file (go.mod, package.json, etc.).
type Layer struct {
	Name     string
	Role     string
	RootDir  string
	Language string
	Files    int
}

// Manifest represents a detected build/dependency manifest file.
type Manifest struct {
	Path     string
	Type     string
	Language string
}

// polyglotThreshold is the minimum number of files a language must have
// to count toward the polyglot determination.
const polyglotThreshold = 5

// DetectStructure scans a list of ingested files for manifest files,
// groups files by the nearest manifest root, and builds layers.
func DetectStructure(files []*ingest.File) *RepoStructure {
	manifests := findManifests(files)

	rs := &RepoStructure{
		Manifests: manifests,
		Languages: make(map[string]int),
	}

	// Count source files per language.
	for _, f := range files {
		if f.Language != "" {
			rs.Languages[f.Language]++
		}
	}

	layers := layersFromManifests(manifests, files)
	if len(layers) == 0 {
		// Fallback: single root layer with dominant language.
		lang := dominantLanguage(rs.Languages)
		layers = []Layer{
			{
				Name:     "root",
				Role:     "",
				RootDir:  ".",
				Language: lang,
				Files:    countSourceFiles(files),
			},
		}
	}

	rs.Layers = layers

	return rs
}

// IsPolyglot reports whether the repository contains 2 or more languages
// each with at least polyglotThreshold source files.
func (rs *RepoStructure) IsPolyglot() bool {
	count := 0
	for _, n := range rs.Languages {
		if n >= polyglotThreshold {
			count++
		}
	}
	return count >= 2
}

// dominantLanguage returns the language with the highest file count.
// Returns "" if the map is empty.
func dominantLanguage(langs map[string]int) string {
	best := ""
	bestCount := 0

	for lang, count := range langs {
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}

	return best
}

// countSourceFiles returns the number of files that have a detected language.
func countSourceFiles(files []*ingest.File) int {
	count := 0
	for _, f := range files {
		if f.Language != "" {
			count++
		}
	}
	return count
}
