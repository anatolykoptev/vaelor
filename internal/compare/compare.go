// Package compare provides structural and semantic code comparison between repositories.
//
// Comparison works at multiple levels:
//   - Symbols: matched by name, signature, and semantic similarity
//   - Metrics: quantitative measures of code quality and complexity
//   - Architecture: LLM-powered insights into design patterns and gaps
package compare

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/llm"
)

// CompareInput is the input for CompareRepos.
type CompareInput struct {
	RootA       string
	RootB       string
	Query       string
	Opts        SnapshotOpts
	OxCodes     *oxcodes.Client
	EmbedClient *embed.Client    // nil = skip semantic matching
	GraphStore  *codegraph.Store // nil = skip architecture graph analysis
}

// compareTimeout is the hard deadline for the entire CompareRepos operation.
// Ensures the tool always returns before the MCP client timeout (~60s).
const compareTimeout = 90 * time.Second

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

// CompareRepos orchestrates a full comparison between two repositories.
// llmClient must be non-nil; pass llm.NoOp{} when LLM is not configured.
// NoOp.Complete returns ErrLLMUnavailable which runLLMAnalysis treats as a
// fallback (deterministic comparison still runs in full).
func CompareRepos(ctx context.Context, input CompareInput, llmClient llm.Completer) (*CompareResult, error) {
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
	var classifier LLMClassifier
	if input.EmbedClient != nil {
		classifier = NewEmbeddingClassifier(ctx, input.EmbedClient)
	}
	matches := MatchSymbols(snapA.Symbols, snapB.Symbols, classifier)

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

	// --- Parallel enrichment: quality + freshness + dataflow + arch ---
	enr := collectEnrichment(ctx, enrichInput{
		rootA:      input.RootA,
		rootB:      input.RootB,
		langA:      snapA.Language,
		langB:      snapB.Language,
		oxCodes:    input.OxCodes,
		graphStore: input.GraphStore,
	})

	// Propagate freshness ratios into metrics (they affect scoring).
	if enr.freshnessA != nil {
		metricsA.DepFreshnessRatio = enr.freshnessA.DepFreshnessRatio
		metricsA.VulnSecurityRatio = enr.freshnessA.VulnSecurityRatio
	}
	if enr.freshnessB != nil {
		metricsB.DepFreshnessRatio = enr.freshnessB.DepFreshnessRatio
		metricsB.VulnSecurityRatio = enr.freshnessB.VulnSecurityRatio
	}

	var analysis LLMAnalysis

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

	// Cross-language report (when repos use different languages).
	var crossLangReport *CrossLangReport
	if snapA.Language != snapB.Language {
		crossLangReport = BuildCrossLangReport(matches, snapA.Language, snapB.Language)
	}

	// LLM analysis runs after all enrichment so it has full context:
	// freshness, dataflow, API surface, and routes.
	// llmClient is always non-nil (NoOp{} when unconfigured); runLLMAnalysis
	// handles ErrLLMUnavailable via its own error branch (compare/llm.go).
	analysis = runLLMAnalysis(ctx, llmClient, matches, metricsA, metricsB, input.Query,
		hotspotsA, hotspotsB, relStatsA, relStatsB,
		enr.freshnessA, enr.freshnessB, enr.dataflowA, enr.dataflowB, apiDiff, routeDiff,
		enr.archMetricsA, enr.archMetricsB)

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
		QualityA:        enr.qualityA,
		QualityB:        enr.qualityB,
		FreshnessA:      enr.freshnessA,
		FreshnessB:      enr.freshnessB,
		DataflowA:       enr.dataflowA,
		DataflowB:       enr.dataflowB,
		APIDiffResult:   apiDiff,
		RouteDiffResult: routeDiff,
		CouplingA:       enr.couplingA,
		CouplingB:       enr.couplingB,
		ArchMetricsA:    enr.archMetricsA,
		ArchMetricsB:    enr.archMetricsB,
		CrossLangReport: crossLangReport,
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
	churn, _ := CollectChurn(ctx, root, 0)
	if churn == nil {
		return nil
	}
	return ComputeHotspots(churn, FileComplexityFromSnapshot(snap))
}
