// Package compare provides structural and semantic code comparison between repositories.
//
// Comparison works at multiple levels:
//   - Architecture: directory layout, layer structure, module boundaries
//   - API: exported symbols, function signatures, interface contracts
//   - Dependencies: import graphs, external module usage, version strategies
//   - Patterns: idioms and patterns detected via AST queries
//   - Quality: complexity metrics, test coverage, documentation density
package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// CompareMode selects what aspect of the repositories is compared.
type CompareMode string

const (
	ModeArchitecture CompareMode = "architecture"
	ModeAPI          CompareMode = "api"
	ModeDependencies CompareMode = "dependencies"
	ModePatterns     CompareMode = "patterns"
	ModeQuality      CompareMode = "quality"
)

// RepoSnapshot is a parsed, summarized view of a repository ready for comparison.
type RepoSnapshot struct {
	// Name is the repository name or slug.
	Name string

	// Root is the absolute local path to the repository.
	Root string

	// Language is the dominant programming language.
	Language string

	// Symbols is all symbols extracted from the repository.
	Symbols []*parser.Symbol

	// Imports is the flat list of all unique import paths used.
	Imports []string

	// FileCount is the total number of source files.
	FileCount int

	// TotalLines is the approximate total lines of code.
	TotalLines int

	// Metrics holds computed quality metrics.
	Metrics RepoMetrics
}

// RepoMetrics holds aggregate quality and complexity metrics for a repo.
type RepoMetrics struct {
	// AvgFunctionLines is the average lines per function.
	AvgFunctionLines float64

	// MaxFunctionLines is the largest function by line count.
	MaxFunctionLines int

	// TestFileRatio is the fraction of files that are test files.
	TestFileRatio float64

	// DocCommentRatio is the fraction of exported symbols with doc comments.
	DocCommentRatio float64

	// ExternalDepsCount is the number of distinct external module dependencies.
	ExternalDepsCount int
}

// CompareResult contains the structured output of a comparison.
type CompareResult struct {
	// RepoA is the name of the first repository.
	RepoA string

	// RepoB is the name of the second repository.
	RepoB string

	// Mode is the comparison mode used.
	Mode CompareMode

	// Summary is the LLM-generated comparison narrative.
	Summary string

	// Similarities is a list of key similarities between the two repos.
	Similarities []string

	// Differences is a structured breakdown of key differences.
	Differences []Difference
}

// Difference describes a single meaningful difference between two repositories.
type Difference struct {
	// Aspect is what is being compared (e.g. "error handling", "HTTP routing").
	Aspect string

	// RepoAValue is how repo A handles this aspect.
	RepoAValue string

	// RepoBValue is how repo B handles this aspect.
	RepoBValue string

	// Significance is the importance level: high, medium, low.
	Significance string
}

// Compare performs a structural comparison of two repository snapshots.
//
// TODO: implement full comparison logic using AST metrics, import graph diff,
// and LLM-powered narrative generation.
func Compare(_ *RepoSnapshot, _ *RepoSnapshot, _ CompareMode) (*CompareResult, error) {
	// TODO: implement
	return &CompareResult{}, nil
}
