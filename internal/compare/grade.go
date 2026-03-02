package compare

import "math"

// Grade thresholds — score out of 100.
const (
	gradeAThreshold = 80
	gradeBThreshold = 60
	gradeCThreshold = 40
	gradeDThreshold = 20
)

// Scoring weights — must sum to 1.0.
const (
	weightComplexity    = 0.25
	weightTestCoverage  = 0.20
	weightDocCoverage   = 0.15
	weightFuncSize      = 0.15
	weightErrorHandling = 0.15
	weightMaxComplexity = 0.10
)

// Target ratios — ideal thresholds for normalization.
const (
	targetTestRatio          = 0.3 // 30% test files is ideal
	targetDocRatio           = 0.6 // 60% documented symbols is ideal
	targetErrorHandlingRatio = 0.6 // 60% error-handling coverage is ideal
)

// GradeScore computes a quality score in [0, 100] from RepoMetrics.
// Higher is better.
func GradeScore(m RepoMetrics) float64 {
	if m.Files == 0 {
		return 0
	}

	// Each sub-score is in [0, 1], where 1 = best.
	complexityScore := clamp01(1.0 - (m.AvgComplexity-3.0)/12.0)
	maxComplexityScore := clamp01(1.0 - (float64(m.MaxComplexity)-8.0)/17.0)
	testScore := clamp01(m.TestRatio / targetTestRatio)
	docScore := clamp01(m.DocRatio / targetDocRatio)
	funcSizeScore := clamp01(1.0 - (m.AvgFuncLines-15.0)/45.0)
	errorScore := clamp01(m.ErrorHandlingRatio / targetErrorHandlingRatio)

	total := complexityScore*weightComplexity +
		maxComplexityScore*weightMaxComplexity +
		testScore*weightTestCoverage +
		docScore*weightDocCoverage +
		funcSizeScore*weightFuncSize +
		errorScore*weightErrorHandling

	return math.Round(total * 100)
}

// ComputeGrade returns a letter grade (A-F) for the given metrics.
func ComputeGrade(m RepoMetrics) string {
	score := GradeScore(m)
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

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
