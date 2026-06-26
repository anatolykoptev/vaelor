package explore

import (
	"math"
	"strings"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// Grade thresholds — must match compare/grade.go.
const (
	gradeAThreshold = 80
	gradeBThreshold = 70
	gradeCThreshold = 60
	gradeDThreshold = 50
)

// Weights redistributed from compare/grade.go (error handling 15% spread across 5 factors).
const (
	healthWeightComplexity    = 0.28
	healthWeightMaxComplexity = 0.12
	healthWeightTestCoverage  = 0.23
	healthWeightDocCoverage   = 0.18
	healthWeightFuncSize      = 0.19
)

const healthPercentScale = 100 // multiplier to convert [0,1] scores to percentage points

// Normalization targets and ranges — subset of compare/grade.go constants.
const (
	healthTargetCyclomaticAvg = 3.0
	healthRangeCyclomaticAvg  = 12.0
	healthTargetCyclomaticMax = 8.0
	healthRangeCyclomaticMax  = 17.0
	healthTargetTestRatio     = 0.3
	healthTargetDocRatio      = 0.6
	healthTargetFuncSize      = 15.0
	healthRangeFuncSize       = 45.0
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

// healthSubScores holds the five normalized sub-scores in [0, 1].
type healthSubScores struct {
	complexity    float64
	maxComplexity float64
	test          float64
	doc           float64
	funcSize      float64
}

// computeHealth produces a health score from already-parsed symbols and files.
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

	ss := computeSubScores(sm, testFiles, len(files))
	score := weighAndRound(ss)
	grade := scoreToGrade(score)

	return &HealthSummary{Score: score, Grade: grade}
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

// computeSubScores derives the five normalized [0, 1] sub-scores from the
// accumulated metrics.  Same formulas as compare/grade.go.
func computeSubScores(sm symbolMetrics, testFiles, fileCount int) healthSubScores {
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

	return healthSubScores{
		complexity:    healthClamp01(1.0 - (avgComplexity-healthTargetCyclomaticAvg)/healthRangeCyclomaticAvg),
		maxComplexity: healthClamp01(1.0 - (float64(sm.maxComplexity)-healthTargetCyclomaticMax)/healthRangeCyclomaticMax),
		test:          healthClamp01(testRatio / healthTargetTestRatio),
		doc:           healthClamp01(docRatio / healthTargetDocRatio),
		funcSize:      healthClamp01(1.0 - (avgFuncLines-healthTargetFuncSize)/healthRangeFuncSize),
	}
}

// weighAndRound applies the five weights to the sub-scores and returns the
// rounded integer score in [0, 100].
func weighAndRound(ss healthSubScores) int {
	total := ss.complexity*healthWeightComplexity +
		ss.maxComplexity*healthWeightMaxComplexity +
		ss.test*healthWeightTestCoverage +
		ss.doc*healthWeightDocCoverage +
		ss.funcSize*healthWeightFuncSize

	return int(math.Round(total * healthPercentScale))
}

// scoreToGrade converts a numeric score to an A-F letter grade.
func scoreToGrade(score int) string {
	switch {
	case score >= gradeAThreshold:
		return "A"
	case score >= gradeBThreshold:
		return "B"
	case score >= gradeCThreshold:
		return "C"
	case score >= gradeDThreshold:
		return "D"
	default:
		return "F"
	}
}

func healthClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
