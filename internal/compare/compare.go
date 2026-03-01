// Package compare provides structural and semantic code comparison between repositories.
//
// Comparison works at multiple levels:
//   - Symbols: matched by name, signature, and semantic similarity
//   - Metrics: quantitative measures of code quality and complexity
//   - Architecture: LLM-powered insights into design patterns and gaps
package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// MatchType describes how two symbols were matched.
type MatchType string

const (
	// MatchExact means the symbols have the same name and kind and identical body.
	MatchExact MatchType = "exact"

	// MatchModified means the symbols have the same name and kind but different body.
	MatchModified MatchType = "modified"

	// MatchRenamed means the symbols have different names but same signature and/or body hash.
	MatchRenamed MatchType = "renamed"

	// MatchFuzzy means the symbols have similar names or signatures.
	MatchFuzzy MatchType = "fuzzy"

	// MatchSemantic means the symbols serve the same purpose (LLM-determined).
	MatchSemantic MatchType = "semantic"

	// MatchGap means the symbol has no counterpart in the other repository.
	MatchGap MatchType = "gap"
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

// MissingIn returns "repo_a" if the symbol is missing from repo A, "repo_b" if missing
// from repo B, or "" if the symbol exists in both (or neither).
func (m SymbolMatch) MissingIn() string {
	switch {
	case m.SymbolA == nil && m.SymbolB != nil:
		return "repo_a"
	case m.SymbolA != nil && m.SymbolB == nil:
		return "repo_b"
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
	AvgComplexity      float64 `json:"avgComplexity"`
	MaxComplexity      int     `json:"maxComplexity"`
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
	RepoA          string      `json:"repo_a"`
	RepoB          string      `json:"repo_b"`
	Query          string      `json:"query"`
	MetricsA       RepoMetrics `json:"metrics_a"`
	MetricsB       RepoMetrics `json:"metrics_b"`
	Analysis       LLMAnalysis `json:"analysis"`
	MatchedSymbols int         `json:"matched_symbols"`
	UnmatchedA     int         `json:"unmatched_a"`
	UnmatchedB     int         `json:"unmatched_b"`
}

// CompareInput is the input for CompareRepos.
type CompareInput struct {
	RootA string
	RootB string
	Query string
	Opts  SnapshotOpts
}

// CompareRepos orchestrates a full comparison between two repositories.
// llmClient may be nil to skip LLM analysis (useful for testing).
func CompareRepos(ctx context.Context, input CompareInput, llmClient *llm.Client) (*CompareResult, error) {
	// Build snapshots in parallel.
	var snapA, snapB *RepoSnapshot
	var errA, errB error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		snapA, errA = BuildSnapshot(ctx, input.RootA, input.Opts)
	}()
	go func() {
		defer wg.Done()
		snapB, errB = BuildSnapshot(ctx, input.RootB, input.Opts)
	}()
	wg.Wait()

	if errA != nil {
		return nil, fmt.Errorf("snapshot repo_a: %w", errA)
	}
	if errB != nil {
		return nil, fmt.Errorf("snapshot repo_b: %w", errB)
	}

	// Match symbols.
	matches := MatchSymbols(snapA.Symbols, snapB.Symbols, nil)

	// Compute metrics.
	metricsA := ComputeMetrics(snapA)
	metricsB := ComputeMetrics(snapB)

	// Count matches and gaps.
	// SymbolA == nil means the symbol exists only in B (missing from A).
	// SymbolB == nil means the symbol exists only in A (missing from B).
	matched, unmatchedA, unmatchedB := 0, 0, 0
	for _, m := range matches {
		switch {
		case m.SymbolB == nil && m.SymbolA != nil:
			unmatchedA++
		case m.SymbolA == nil && m.SymbolB != nil:
			unmatchedB++
		case m.SymbolA != nil && m.SymbolB != nil:
			matched++
		}
	}

	result := &CompareResult{
		RepoA:          snapA.Name,
		RepoB:          snapB.Name,
		Query:          input.Query,
		MetricsA:       metricsA,
		MetricsB:       metricsB,
		MatchedSymbols: matched,
		UnmatchedA:     unmatchedA,
		UnmatchedB:     unmatchedB,
	}

	// LLM analysis (optional). Errors are non-fatal — structural results are
	// always returned even when the LLM is unavailable.
	if llmClient != nil {
		compareCtx := BuildCompareContext(matches, metricsA, metricsB, input.Query)
		answer, err := llmClient.Complete(ctx, llm.SystemPromptCodeCompare, compareCtx)
		if err == nil {
			result.Analysis = parseAnalysis(answer)
		} else {
			result.Analysis = LLMAnalysis{
				Recommendations: []string{fmt.Sprintf("LLM analysis unavailable: %v", err)},
			}
		}
	}

	return result, nil
}

// parseAnalysis tries to parse LLM response as JSON LLMAnalysis.
// Falls back to wrapping raw text in recommendations.
func parseAnalysis(raw string) LLMAnalysis {
	cleaned := extractJSON(raw)

	var analysis LLMAnalysis
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		return LLMAnalysis{
			Recommendations: []string{raw},
		}
	}
	return analysis
}

// extractJSON tries to extract a JSON block from markdown-wrapped LLM output.
func extractJSON(s string) string {
	start := strings.Index(s, "```json")
	if start >= 0 {
		s = s[start+7:]
		end := strings.Index(s, "```")
		if end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	start = strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
