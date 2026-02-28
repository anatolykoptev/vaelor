// Package compare provides structural and semantic code comparison between repositories.
//
// Comparison works at multiple levels:
//   - Symbols: matched by name, signature, and semantic similarity
//   - Metrics: quantitative measures of code quality and complexity
//   - Architecture: LLM-powered insights into design patterns and gaps
package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// MatchType describes how two symbols were matched.
type MatchType string

const (
	// MatchExact means the symbols have the same name and kind.
	MatchExact MatchType = "exact"

	// MatchFuzzy means the symbols have similar names or signatures.
	MatchFuzzy MatchType = "fuzzy"

	// MatchSemantic means the symbols serve the same purpose (LLM-determined).
	MatchSemantic MatchType = "semantic"
)

// SymbolMatch pairs a symbol from repo A with its counterpart in repo B.
// If one side is nil, the symbol exists only in the other repository.
type SymbolMatch struct {
	SymbolA   *parser.Symbol `json:"symbolA,omitempty"`
	SymbolB   *parser.Symbol `json:"symbolB,omitempty"`
	MatchType MatchType      `json:"matchType"`
	Category  string         `json:"category"`
	Score     float64        `json:"score"`
}

// IsGap returns true if the symbol is present in only one repository (or neither).
func (m SymbolMatch) IsGap() bool {
	return m.SymbolA == nil || m.SymbolB == nil
}

// MissingIn returns "A" if the symbol is missing from repo A, "B" if missing
// from repo B, or "" if the symbol exists in both (or neither).
func (m SymbolMatch) MissingIn() string {
	switch {
	case m.SymbolA != nil && m.SymbolB == nil:
		return "B"
	case m.SymbolA == nil && m.SymbolB != nil:
		return "A"
	default:
		return ""
	}
}

// SnapshotFile holds parsed metadata for a single source file within a repo snapshot.
type SnapshotFile struct {
	RelPath  string           `json:"relPath"`
	Language string           `json:"language"`
	Lines    int              `json:"lines"`
	Symbols  []*parser.Symbol `json:"symbols,omitempty"`
	Imports  []string         `json:"imports,omitempty"`
}

// RepoSnapshot is a parsed, summarized view of a repository ready for comparison.
type RepoSnapshot struct {
	// Name is the repository name or slug.
	Name string `json:"name"`

	// Root is the absolute local path to the repository.
	Root string `json:"root"`

	// Language is the dominant programming language.
	Language string `json:"language"`

	// Symbols is all symbols extracted from the repository.
	Symbols []*parser.Symbol `json:"symbols,omitempty"`

	// Imports is the flat list of all unique import paths used.
	Imports []string `json:"imports,omitempty"`

	// Files holds per-file metadata.
	Files []SnapshotFile `json:"files,omitempty"`

	// FileCount is the total number of source files.
	FileCount int `json:"fileCount"`

	// TotalLines is the approximate total lines of code.
	TotalLines int `json:"totalLines"`
}

// RepoMetrics holds aggregate quality and complexity metrics for a repo.
type RepoMetrics struct {
	Files              int     `json:"files"`
	TotalLines         int     `json:"totalLines"`
	AvgFuncLines       float64 `json:"avgFuncLines"`
	MaxFuncLines       int     `json:"maxFuncLines"`
	TestRatio          float64 `json:"testRatio"`
	DocRatio           float64 `json:"docRatio"`
	ErrorHandlingRatio float64 `json:"errorHandlingRatio"`
	Interfaces         int     `json:"interfaces"`
	ExternalDeps       int     `json:"externalDeps"`
}

// QualityAspect describes a qualitative comparison point between two repos.
type QualityAspect struct {
	Aspect   string `json:"aspect"`
	Winner   string `json:"winner"`
	Reason   string `json:"reason"`
	SnippetA string `json:"snippetA,omitempty"`
	SnippetB string `json:"snippetB,omitempty"`
}

// CoverageGap identifies a feature present in one repo but absent in the other.
type CoverageGap struct {
	MissingIn  string `json:"missingIn"`
	Feature    string `json:"feature"`
	LocationB  string `json:"locationB,omitempty"`
	Importance string `json:"importance"`
}

// ArchitectureInsight captures a design pattern or architectural observation.
type ArchitectureInsight struct {
	Insight string `json:"insight"`
	Source  string `json:"source"`
	Example string `json:"example,omitempty"`
	Benefit string `json:"benefit"`
}

// LLMAnalysis holds the structured output from LLM-powered comparison.
type LLMAnalysis struct {
	Quality         []QualityAspect       `json:"quality"`
	Gaps            []CoverageGap         `json:"gaps"`
	Architecture    []ArchitectureInsight  `json:"architecture"`
	Recommendations []string              `json:"recommendations"`
}

// CompareResult contains the full structured output of a code comparison.
type CompareResult struct {
	RepoA          string         `json:"repoA"`
	RepoB          string         `json:"repoB"`
	Query          string         `json:"query"`
	MetricsA       RepoMetrics    `json:"metricsA"`
	MetricsB       RepoMetrics    `json:"metricsB"`
	Analysis       LLMAnalysis    `json:"analysis"`
	MatchedSymbols []SymbolMatch  `json:"matchedSymbols"`
	UnmatchedA     []SymbolMatch  `json:"unmatchedA"`
	UnmatchedB     []SymbolMatch  `json:"unmatchedB"`
}
