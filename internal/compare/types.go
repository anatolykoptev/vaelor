package compare

import (
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

// DiffStats aggregates AST diff statistics across all modified matches.
type DiffStats struct {
	ModifiedWithDiff int `json:"modifiedWithDiff"`
	TotalInserts     int `json:"totalInserts"`
	TotalDeletes     int `json:"totalDeletes"`
	TotalUpdates     int `json:"totalUpdates"`
	TotalMoves       int `json:"totalMoves"`
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

	// Partial is true when one or more ingested files could not be read during
	// snapshotting (e.g. the source tree was deleted out from under the walk, or
	// a per-file context cancellation cut the parse short). When set, the derived
	// metrics (Files, TotalLines, TestRatio, Symbols) under-count the real repo
	// and MUST NOT be presented as a complete result. See DroppedReadError /
	// DroppedCtxCancel for the per-reason breakdown.
	Partial bool `json:"partial,omitempty"`

	// DroppedReadError counts files that were enumerated by ingest but failed
	// os.ReadFile at parse time (vanished file / permission flip / truncated
	// tree). This is the dominant signal of a use-after-delete race on a shared
	// clone dir.
	DroppedReadError int `json:"droppedReadError,omitempty"`

	// DroppedCtxCancel counts files skipped because the snapshot context was
	// cancelled before the parse worker reached them (timeout / caller abort).
	DroppedCtxCancel int `json:"droppedCtxCancel,omitempty"`
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
	TotalDeps              int     `json:"totalDeps,omitempty"`         // number of external dependencies scanned; 0 means no manifests found (N/A)
	// DeadCodeCandidates is the count of functions with CE dead-code probability >= 0.25.
	// Populated from code_dead_code_scores when code_graph snapshot is available.
	DeadCodeCandidates int      `json:"deadCodeCandidates,omitempty"`
	DeadCodeTopNames   []string `json:"deadCodeTopNames,omitempty"` // up to 3 top candidates
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

// VerdictResult is the structured "can replace?" assessment.
type VerdictResult struct {
	CanReplace string   `json:"canReplace"` // "yes", "partial", "no"
	Reason     string   `json:"reason"`
	Blockers   []string `json:"blockers,omitempty"`
}

// LLMAnalysis holds the structured output from LLM-powered comparison.
type LLMAnalysis struct {
	Quality         []QualityAspect       `json:"quality"`
	Gaps            []CoverageGap         `json:"gaps"`
	Architecture    []ArchitectureInsight `json:"architecture"`
	Recommendations []string              `json:"recommendations"`
	Verdict         VerdictResult         `json:"verdict"`
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
	CouplingA       []CoupledPair      `json:"coupling_a,omitempty"`
	CouplingB       []CoupledPair      `json:"coupling_b,omitempty"`
	ArchMetricsA    *ArchMetrics       `json:"arch_metrics_a,omitempty"`
	ArchMetricsB    *ArchMetrics       `json:"arch_metrics_b,omitempty"`
	CrossLangReport *CrossLangReport   `json:"cross_lang_report,omitempty"`
}
