package explore

import (
	"math"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/ingest"
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
	healthWeightComplexity    = 0.28 // was 0.25
	healthWeightMaxComplexity = 0.12 // was 0.10
	healthWeightTestCoverage  = 0.23 // was 0.20
	healthWeightDocCoverage   = 0.18 // was 0.15
	healthWeightFuncSize      = 0.19 // was 0.15
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
		funcCount      int
		totalComplexity int
		maxComplexity  int
		totalFuncLines int
		exportedCount  int
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
	testFiles := 0
	for _, f := range files {
		base := filepath.Base(f.RelPath)
		if strings.HasSuffix(base, "_test.go") ||
			strings.HasPrefix(base, "test_") ||
			strings.Contains(base, ".test.") ||
			strings.Contains(base, ".spec.") {
			testFiles++
		}
	}

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
	complexityScore := healthClamp01(1.0 - (avgComplexity-3.0)/12.0)
	maxComplexityScore := healthClamp01(1.0 - (float64(maxComplexity)-8.0)/17.0)
	testScore := healthClamp01(testRatio / 0.3)
	docScore := healthClamp01(docRatio / 0.6)
	funcSizeScore := healthClamp01(1.0 - (avgFuncLines-15.0)/45.0)

	total := complexityScore*healthWeightComplexity +
		maxComplexityScore*healthWeightMaxComplexity +
		testScore*healthWeightTestCoverage +
		docScore*healthWeightDocCoverage +
		funcSizeScore*healthWeightFuncSize

	score := int(math.Round(total * 100))

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
