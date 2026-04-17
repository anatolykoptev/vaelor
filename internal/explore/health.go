package explore

import (
	"math"
	"strings"
	"unicode"

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

// computeHealth produces a health score from already-parsed symbols and files.
func computeHealth(symbols []*parser.Symbol, files []*ingest.File) *HealthSummary {
	if len(files) == 0 {
		return nil
	}

	var (
		funcCount       int
		totalComplexity int
		maxComplexity   int
		totalFuncLines  int
		exportedCount   int
		documentedCount int
	)

	for _, sym := range symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			funcCount++
			totalComplexity += sym.Complexity
			if sym.Complexity > maxComplexity {
				maxComplexity = sym.Complexity
			}
			if sym.EndLine > sym.StartLine {
				totalFuncLines += int(sym.EndLine - sym.StartLine)
			}
		}
		if isExportedName(sym.Name) {
			exportedCount++
			if sym.DocComment != "" {
				documentedCount++
			}
		}
	}

	// Test ratio: test files / total files.
	// Collect files containing Rust test attributes (#[test], #[cfg(test)]).
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

	// Invariants: counters must be non-negative; divisions guarded below.
	goutil.Assertf(funcCount >= 0, "funcCount must be >= 0, got %d", funcCount)
	goutil.Assertf(testFiles >= 0 && testFiles <= len(files),
		"testFiles %d out of range [0, %d]", testFiles, len(files))
	goutil.Assertf(documentedCount >= 0 && documentedCount <= exportedCount,
		"documentedCount %d > exportedCount %d", documentedCount, exportedCount)

	var avgComplexity, avgFuncLines, testRatio, docRatio float64
	if funcCount > 0 {
		avgComplexity = float64(totalComplexity) / float64(funcCount)
		avgFuncLines = float64(totalFuncLines) / float64(funcCount)
	}
	if len(files) > 0 {
		testRatio = float64(testFiles) / float64(len(files))
	}
	if exportedCount > 0 {
		docRatio = float64(documentedCount) / float64(exportedCount)
	}

	// Sub-scores in [0, 1] — same formulas as compare/grade.go.
	complexityScore := healthClamp01(1.0 - (avgComplexity-healthTargetCyclomaticAvg)/healthRangeCyclomaticAvg)
	maxComplexityScore := healthClamp01(1.0 - (float64(maxComplexity)-healthTargetCyclomaticMax)/healthRangeCyclomaticMax)
	testScore := healthClamp01(testRatio / healthTargetTestRatio)
	docScore := healthClamp01(docRatio / healthTargetDocRatio)
	funcSizeScore := healthClamp01(1.0 - (avgFuncLines-healthTargetFuncSize)/healthRangeFuncSize)

	total := complexityScore*healthWeightComplexity +
		maxComplexityScore*healthWeightMaxComplexity +
		testScore*healthWeightTestCoverage +
		docScore*healthWeightDocCoverage +
		funcSizeScore*healthWeightFuncSize

	score := int(math.Round(total * healthPercentScale))

	var grade string
	switch {
	case score >= gradeAThreshold:
		grade = "A"
	case score >= gradeBThreshold:
		grade = "B"
	case score >= gradeCThreshold:
		grade = "C"
	case score >= gradeDThreshold:
		grade = "D"
	default:
		grade = "F"
	}

	return &HealthSummary{Score: score, Grade: grade}
}

func isExportedName(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
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
