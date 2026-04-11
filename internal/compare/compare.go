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
	"time"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/anatolykoptev/go-kit/llm"
)

// qualityTimeout limits how long enrichment checks (quality, freshness, dataflow) can run.
// All enrichment is informational — must never block core comparison.
const qualityTimeout = 7 * time.Second

// compareTimeout is the hard deadline for the entire CompareRepos operation.
// Ensures the tool always returns before the MCP client timeout (~60s).
const compareTimeout = 50 * time.Second

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

	// MatchMoved means the symbol has the same name, kind, and body but resides
	// in a different file or package. Indicates code reorganization without logic change.
	MatchMoved MatchType = "moved"

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
	Diff      *DiffSummary   `json:"diff,omitempty"`
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

// annotateASTDiffs computes AST diffs for modified symbol matches.
func annotateASTDiffs(matches []SymbolMatch) {
	for i := range matches {
		m := &matches[i]
		if m.MatchType != MatchModified || m.SymbolA == nil || m.SymbolB == nil {
			continue
		}
		lang := m.SymbolA.Language
		if lang == "" {
			lang = m.SymbolB.Language
		}
		m.Diff = ComputeASTDiff(m.SymbolA.Body, m.SymbolB.Body, lang)
	}
}

// DiffStats aggregates AST diff statistics across all modified matches.
type DiffStats struct {
	ModifiedWithDiff int `json:"modifiedWithDiff"`
	TotalInserts     int `json:"totalInserts"`
	TotalDeletes     int `json:"totalDeletes"`
	TotalUpdates     int `json:"totalUpdates"`
	TotalMoves       int `json:"totalMoves"`
}

func computeDiffStats(matches []SymbolMatch) *DiffStats {
	stats := &DiffStats{}
	for _, m := range matches {
		if m.Diff == nil {
			continue
		}
		stats.ModifiedWithDiff++
		stats.TotalInserts += m.Diff.Inserts
		stats.TotalDeletes += m.Diff.Deletes
		stats.TotalUpdates += m.Diff.Updates
		stats.TotalMoves += m.Diff.Moves
	}
	if stats.ModifiedWithDiff == 0 {
		return nil
	}
	return stats
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

	// FocusMode is "content" when the fallback content filter was used
	// instead of path-based focus. Empty means path-based or no focus.
	FocusMode string `json:"focusMode,omitempty"`

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

	// Rels holds type relationships extracted from the repository.
	Rels []parser.TypeRelationship `json:"rels,omitempty"`
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
	Grade              string  `json:"grade"`

	// New metrics for smarter scoring.
	AvgCognitiveComplexity float64 `json:"avgCognitiveComplexity"`
	MaxCognitiveComplexity int     `json:"maxCognitiveComplexity"`
	AvgNestingDepth        float64 `json:"avgNestingDepth"`
	MaxNestingDepth        int     `json:"maxNestingDepth"`
	LargeFileRatio         float64 `json:"largeFileRatio"`
	DuplicationRatio       float64 `json:"duplicationRatio"`
	MagicNumberRatio       float64 `json:"magicNumberRatio"`
	AvgParamCount          float64 `json:"avgParamCount"`
	MaxParamCount          int     `json:"maxParamCount"`
	Score                  float64 `json:"score"`
	SemanticDupRatio       float64 `json:"semanticDupRatio,omitempty"`  // fraction of functions in semantic dup groups
	DepFreshnessRatio      float64 `json:"depFreshnessRatio,omitempty"` // fraction of deps that are up-to-date
	VulnSecurityRatio      float64 `json:"vulnSecurityRatio,omitempty"` // fraction of deps with no known vulns
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
	Architecture    []ArchitectureInsight `json:"architecture"`
	Recommendations []string              `json:"recommendations"`
}

// MatchBreakdown counts matches by type for structured reporting.
type MatchBreakdown struct {
	Exact    int `json:"exact"`
	Modified int `json:"modified"`
	Fuzzy    int `json:"fuzzy"`
	Renamed  int `json:"renamed"`
	Semantic int `json:"semantic"`
	Moved    int `json:"moved"`
}

// CompareResult contains the full structured output of a code comparison.
type CompareResult struct {
	RepoA           string             `json:"repo_a"`
	RepoB           string             `json:"repo_b"`
	Query           string             `json:"query"`
	MetricsA        RepoMetrics        `json:"metrics_a"`
	MetricsB        RepoMetrics        `json:"metrics_b"`
	Analysis        LLMAnalysis        `json:"analysis"`
	MatchedSymbols  int                `json:"matched_symbols"`
	UnmatchedA      int                `json:"unmatched_a"`
	UnmatchedB      int                `json:"unmatched_b"`
	MatchBreakdown  MatchBreakdown     `json:"match_breakdown"`
	ImportDiff      ImportDiff         `json:"import_diff"`
	DiffStats       *DiffStats         `json:"diff_stats,omitempty"`
	HotspotsA       []HotspotFile      `json:"hotspots_a,omitempty"`
	HotspotsB       []HotspotFile      `json:"hotspots_b,omitempty"`
	RelStatsA       *RelStats          `json:"rel_stats_a,omitempty"`
	RelStatsB       *RelStats          `json:"rel_stats_b,omitempty"`
	QualityA        *QualityIndicators `json:"quality_a,omitempty"`
	QualityB        *QualityIndicators `json:"quality_b,omitempty"`
	FreshnessA      *FreshnessStats    `json:"freshness_a,omitempty"`
	FreshnessB      *FreshnessStats    `json:"freshness_b,omitempty"`
	DataflowA       *DataflowStats     `json:"dataflow_a,omitempty"`
	DataflowB       *DataflowStats     `json:"dataflow_b,omitempty"`
	APIDiffResult   *APIDiff           `json:"api_diff,omitempty"`
	RouteDiffResult *RouteDiff         `json:"route_diff,omitempty"`
}

// CompareInput is the input for CompareRepos.
type CompareInput struct {
	RootA   string
	RootB   string
	Query   string
	Opts    SnapshotOpts
	OxCodes *oxcodes.Client
}

// CompareRepos orchestrates a full comparison between two repositories.
// llmClient may be nil to skip LLM analysis (useful for testing).
func CompareRepos(ctx context.Context, input CompareInput, llmClient *llm.Client) (*CompareResult, error) {
	// Hard deadline: ensure we always return before MCP client timeout.
	ctx, cancel := context.WithTimeout(ctx, compareTimeout)
	defer cancel()

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

	// Annotate modified matches with AST diffs.
	annotateASTDiffs(matches)

	// Compute metrics.
	metricsA := ComputeMetrics(snapA)
	metricsB := ComputeMetrics(snapB)

	// Compute import diff.
	importDiff := ComputeImportDiff(snapA.Imports, snapB.Imports, snapA.Language)

	// Hotspot analysis (non-fatal — skip if git unavailable).
	hotspotsA := collectHotspots(ctx, input.RootA, snapA)
	hotspotsB := collectHotspots(ctx, input.RootB, snapB)

	// Compute type relationship stats.
	relStatsA := ComputeRelStats(snapA.Rels)
	relStatsB := ComputeRelStats(snapB.Rels)

	// Count matches and gaps.
	matched, unmatchedA, unmatchedB, breakdown := countMatches(matches)

	diffStats := computeDiffStats(matches)

	// --- Parallel enrichment: quality + freshness + dataflow ---
	// All are non-fatal and independent. LLM runs AFTER enrichment so it has
	// freshness, dataflow, API surface, and route data in its context.
	var (
		qualityA, qualityB     *QualityIndicators
		freshnessA, freshnessB *FreshnessStats
		dataflowA, dataflowB   *DataflowStats
		analysis               LLMAnalysis
	)
	{
		ectx, ecancel := context.WithTimeout(ctx, qualityTimeout)
		defer ecancel()

		var ewg sync.WaitGroup

		if input.OxCodes != nil {
			ewg.Add(4)
			go func() {
				defer ewg.Done()
				qualityA = GatherQualityIndicators(ectx, input.OxCodes, input.RootA, snapA.Language)
			}()
			go func() {
				defer ewg.Done()
				qualityB = GatherQualityIndicators(ectx, input.OxCodes, input.RootB, snapB.Language)
			}()
			go func() { defer ewg.Done(); dataflowA = GatherDataflow(ectx, input.OxCodes, input.RootA, snapA.Language) }()
			go func() { defer ewg.Done(); dataflowB = GatherDataflow(ectx, input.OxCodes, input.RootB, snapB.Language) }()
		}

		ewg.Add(2)
		go func() {
			defer ewg.Done()
			fs, depRatio, vulnRatio := collectFreshness(ectx, input.RootA)
			freshnessA = fs
			if fs != nil {
				metricsA.DepFreshnessRatio = depRatio
				metricsA.VulnSecurityRatio = vulnRatio
			}
		}()
		go func() {
			defer ewg.Done()
			fs, depRatio, vulnRatio := collectFreshness(ectx, input.RootB)
			freshnessB = fs
			if fs != nil {
				metricsB.DepFreshnessRatio = depRatio
				metricsB.VulnSecurityRatio = vulnRatio
			}
		}()

		ewg.Wait()
	}

	// API surface diff (fast, no I/O).
	apiSurfA := ExtractAPISurface(snapA.Symbols, snapA.Language)
	apiSurfB := ExtractAPISurface(snapB.Symbols, snapB.Language)
	var apiDiff *APIDiff
	if len(apiSurfA) > 0 || len(apiSurfB) > 0 {
		d := ComputeAPIDiff(apiSurfA, apiSurfB)
		apiDiff = &d
	}

	// Route comparison (reads files, but fast).
	routesA := ExtractRoutes(ctx, input.RootA, snapA)
	routesB := ExtractRoutes(ctx, input.RootB, snapB)
	var routeDiff *RouteDiff
	if len(routesA) > 0 || len(routesB) > 0 {
		d := ComputeRouteDiff(routesA, routesB)
		routeDiff = &d
	}

	// LLM analysis runs after all enrichment so it has full context:
	// freshness, dataflow, API surface, and routes.
	if llmClient != nil {
		analysis = runLLMAnalysis(ctx, llmClient, matches, metricsA, metricsB, input.Query,
			hotspotsA, hotspotsB, relStatsA, relStatsB,
			freshnessA, freshnessB, dataflowA, dataflowB, apiDiff, routeDiff)
	}

	result := &CompareResult{
		RepoA:           snapA.Name,
		RepoB:           snapB.Name,
		Query:           input.Query,
		MetricsA:        metricsA,
		MetricsB:        metricsB,
		Analysis:        analysis,
		MatchedSymbols:  matched,
		UnmatchedA:      unmatchedA,
		UnmatchedB:      unmatchedB,
		MatchBreakdown:  breakdown,
		ImportDiff:      importDiff,
		DiffStats:       diffStats,
		HotspotsA:       hotspotsA,
		HotspotsB:       hotspotsB,
		RelStatsA:       relStatsA,
		RelStatsB:       relStatsB,
		QualityA:        qualityA,
		QualityB:        qualityB,
		FreshnessA:      freshnessA,
		FreshnessB:      freshnessB,
		DataflowA:       dataflowA,
		DataflowB:       dataflowB,
		APIDiffResult:   apiDiff,
		RouteDiffResult: routeDiff,
	}

	return result, nil
}

// countMatches tallies matched, unmatched-A, unmatched-B counts and a
// per-type breakdown from the symbol match list.
func countMatches(matches []SymbolMatch) (matched, unmatchedA, unmatchedB int, breakdown MatchBreakdown) {
	// SymbolA == nil means the symbol exists only in B (missing from A).
	// SymbolB == nil means the symbol exists only in A (missing from B).
	for _, m := range matches {
		switch {
		case m.SymbolB == nil && m.SymbolA != nil:
			unmatchedA++
		case m.SymbolA == nil && m.SymbolB != nil:
			unmatchedB++
		case m.SymbolA != nil && m.SymbolB != nil:
			matched++
			switch m.MatchType {
			case MatchExact:
				breakdown.Exact++
			case MatchModified:
				breakdown.Modified++
			case MatchFuzzy:
				breakdown.Fuzzy++
			case MatchRenamed:
				breakdown.Renamed++
			case MatchSemantic:
				breakdown.Semantic++
			case MatchMoved:
				breakdown.Moved++
			}
		}
	}
	return matched, unmatchedA, unmatchedB, breakdown
}

// collectHotspots runs git churn analysis and computes hotspot files for a single repo.
// Returns nil if git is unavailable or the repo has no churn data.
func collectHotspots(ctx context.Context, root string, snap *RepoSnapshot) []HotspotFile {
	churn, _ := CollectChurn(ctx, root)
	if churn == nil {
		return nil
	}
	return ComputeHotspots(churn, FileComplexityFromSnapshot(snap))
}

// runLLMAnalysis sends the comparison context to the LLM and parses its response.
// Returns a fallback analysis with the error message if the LLM call fails.
func runLLMAnalysis(ctx context.Context, client *llm.Client, matches []SymbolMatch,
	metricsA, metricsB RepoMetrics, query string,
	hotspotsA, hotspotsB []HotspotFile, relStatsA, relStatsB *RelStats,
	freshnessA, freshnessB *FreshnessStats, dataflowA, dataflowB *DataflowStats,
	apiDiff *APIDiff, routeDiff *RouteDiff) LLMAnalysis {
	compareCtx := BuildCompareContextV2(matches, metricsA, metricsB, query,
		hotspotsA, hotspotsB, relStatsA, relStatsB,
		freshnessA, freshnessB, dataflowA, dataflowB, apiDiff, routeDiff)
	answer, err := client.Complete(ctx, prompts.SystemPromptCodeCompare, compareCtx)
	if err != nil {
		return LLMAnalysis{
			Recommendations: []string{fmt.Sprintf("LLM analysis unavailable: %v", err)},
		}
	}
	return parseAnalysis(answer)
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
