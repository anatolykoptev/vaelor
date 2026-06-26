package explore

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// HealthSummary is a lightweight code quality score for explore results.
type HealthSummary struct {
	Score int    `json:"score"`
	Grade string `json:"grade"`
}

// symbolMetrics holds per-symbol counters accumulated by collectSymbolMetrics.
type symbolMetrics struct {
	funcCount       int
	totalComplexity int
	maxComplexity   int
	totalFuncLines  int
	exportedCount   int
	documentedCount int
}

// computeHealth produces a health score from already-parsed symbols and files.
// It delegates to compare.GradeScore (14-factor formula) via buildExploreRepoMetrics.
func computeHealth(symbols []*parser.Symbol, files []*ingest.File) *HealthSummary {
	if len(files) == 0 {
		return nil
	}

	sm := collectSymbolMetrics(symbols)
	testFiles := collectTestFileCount(symbols, files)

	// Invariants: counters must be non-negative; divisions guarded below.
	goutil.Assertf(sm.funcCount >= 0, "funcCount must be >= 0, got %d", sm.funcCount)
	goutil.Assertf(testFiles >= 0 && testFiles <= len(files),
		"testFiles %d out of range [0, %d]", testFiles, len(files))
	goutil.Assertf(sm.documentedCount >= 0 && sm.documentedCount <= sm.exportedCount,
		"documentedCount %d > exportedCount %d", sm.documentedCount, sm.exportedCount)

	rm := buildExploreRepoMetrics(sm, testFiles, len(files))
	score := int(compare.GradeScore(rm))
	grade := compare.ComputeGrade(rm)

	return &HealthSummary{Score: score, Grade: grade}
}

// buildExploreRepoMetrics maps the explore-local symbolMetrics into a
// compare.RepoMetrics value.  Fields that explore does not track (cognitive
// complexity, nesting, error handling, duplication, etc.) are left at their
// zero values, which represent "best-case / unknown" in GradeScore's formulas.
func buildExploreRepoMetrics(sm symbolMetrics, testFiles, fileCount int) compare.RepoMetrics {
	var avgComplexity, avgFuncLines, testRatio, docRatio float64
	if sm.funcCount > 0 {
		avgComplexity = float64(sm.totalComplexity) / float64(sm.funcCount)
		avgFuncLines = float64(sm.totalFuncLines) / float64(sm.funcCount)
	}
	if fileCount > 0 {
		testRatio = float64(testFiles) / float64(fileCount)
	}
	if sm.exportedCount > 0 {
		docRatio = float64(sm.documentedCount) / float64(sm.exportedCount)
	}

	return compare.RepoMetrics{
		Files:         fileCount,
		AvgComplexity: avgComplexity,
		MaxComplexity: sm.maxComplexity,
		AvgFuncLines:  avgFuncLines,
		TestRatio:     testRatio,
		DocRatio:      docRatio,
		// Fields not tracked by explore's lightweight pass are left at 0.
		// Impact on GradeScore:
		//   - ErrorHandlingRatio=0: worst-case (0/target=0), subtracts 0.08 of total weight.
		//   - DepFreshnessRatio=0:  worst-case (0/target=0), subtracts 0.06 of total weight.
		//   - VulnSecurityRatio=0:  worst-case (0/target=0), subtracts 0.06 of total weight.
		//   - LargeFileRatio=0, DuplicationRatio=0, MagicNumberRatio=0, SemanticDupRatio=0:
		//     best-case (1-ratio*mult=1), which is correct since explore does not detect these.
		// Net effect: explore scores are capped at approximately 80 (A), never A+.
		// This is an accepted tradeoff for explore's lightweight context.
	}
}

// collectSymbolMetrics accumulates function-level and export/doc counters from
// the symbol list.  Pure function: reads symbols, returns counters.
func collectSymbolMetrics(symbols []*parser.Symbol) symbolMetrics {
	var sm symbolMetrics
	for _, sym := range symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			sm.funcCount++
			sm.totalComplexity += sym.Complexity
			if sym.Complexity > sm.maxComplexity {
				sm.maxComplexity = sym.Complexity
			}
			if sym.EndLine > sym.StartLine {
				sm.totalFuncLines += int(sym.EndLine - sym.StartLine)
			}
		}
		if langutil.IsExportedForDoc(sym.Name, sym.Language, sym.IsPublic) {
			sm.exportedCount++
			if sym.DocComment != "" {
				sm.documentedCount++
			}
		}
	}
	return sm
}

// collectTestFileCount returns how many files in the repo are test files.
// It recognises both Go/Python/Rust test-file naming conventions (via
// langutil.IsTestFile) and Rust inline test modules (via #[test] attributes).
func collectTestFileCount(symbols []*parser.Symbol, files []*ingest.File) int {
	// Collect files that contain Rust test attributes (#[test], #[cfg(test)]).
	rustTestFiles := make(map[string]struct{})
	for _, sym := range symbols {
		for _, attr := range sym.Attributes {
			if strings.Contains(attr, "test") {
				rustTestFiles[sym.File] = struct{}{}
				break
			}
		}
	}

	testFiles := 0
	for _, f := range files {
		if langutil.IsTestFile(f.RelPath) {
			testFiles++
			continue
		}
		// Check if any symbol in this file has a test attribute (Rust).
		if _, ok := rustTestFiles[f.Path]; ok {
			testFiles++
		}
	}
	return testFiles
}

