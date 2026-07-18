// Package polyglot detects multi-language repository structures by scanning
// manifest files (go.mod, package.json, Cargo.toml, etc.) and grouping
// source files into language-specific layers.
package polyglot

import (
	"github.com/anatolykoptev/vaelor/internal/ingest"
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
		lang := DominantLanguageFromCounts(rs.Languages)
		layers = []Layer{
			{
				Name:     "root",
				Role:     "",
				RootDir:  ".",
				Language: lang,
				Files:    ingest.CountSourceFiles(files),
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

// DominantLanguage returns the most common language among the given files,
// counting only files with a non-empty Language. Returns "" if files is
// empty or no file has a recognised language.
//
// This is the canonical implementation shared by every package that needs
// "most frequent language among a file set" — do not reimplement the
// argmax locally.
func DominantLanguage(files []*ingest.File) string {
	counts := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			counts[f.Language]++
		}
	}
	return DominantLanguageFromCounts(counts)
}

// DominantLanguageFromCounts returns the language with the highest count.
// Returns "" if the map is empty. Ties resolve by Go's non-deterministic
// map iteration order, matching prior per-package implementations.
func DominantLanguageFromCounts(counts map[string]int) string {
	best := ""
	bestCount := 0

	for lang, count := range counts {
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}

	return best
}
