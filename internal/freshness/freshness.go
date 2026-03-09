// Package freshness parses dependency manifest files for multiple languages
// and discovers them in repository directory trees.
package freshness

// Dependency represents a single declared dependency in a manifest file.
type Dependency struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Language string `json:"language"`
}

// ManifestInfo holds parsed metadata from a single manifest file.
type ManifestInfo struct {
	Language       string       `json:"language"`
	RuntimeVersion string       `json:"runtimeVersion,omitempty"`
	Dependencies   []Dependency `json:"dependencies"`
	ManifestPath   string       `json:"manifestPath"`
}

// FreshnessResult summarizes the freshness state of a project's dependencies.
type FreshnessResult struct {
	Total         int           `json:"total"`
	UpToDate      int           `json:"upToDate"`
	MinorOutdated int           `json:"minorOutdated"`
	MajorOutdated int           `json:"majorOutdated"`
	Ratio         float64       `json:"ratio"`
	RuntimeStatus string        `json:"runtimeStatus"`
	Outdated      []OutdatedDep `json:"outdated,omitempty"`
}

// OutdatedDep describes a single outdated dependency.
type OutdatedDep struct {
	Name    string `json:"name"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Kind    string `json:"kind"` // "minor" or "major"
}
