package compare

import "math"

// Grade thresholds — score out of 100.
const (
	gradeAPlusThreshold = 90
	gradeAThreshold     = 80
	gradeBThreshold     = 70
	gradeCThreshold     = 60
	gradeDThreshold     = 50
)

// Scoring weights — 14 sub-scores, must sum to 1.0.
const (
	weightCognitiveComplexity = 0.12
	weightCyclomaticAvg       = 0.07
	weightCyclomaticMax       = 0.05
	weightTestCoverage        = 0.12
	weightDocCoverage         = 0.09
	weightFuncSize            = 0.08
	weightErrorHandling       = 0.08
	weightNestingDepth        = 0.07
	weightFileSize            = 0.07
	weightDuplication         = 0.05
	weightMagicNumbers        = 0.06
	weightSemanticDup         = 0.02
	weightDepFreshness        = 0.06
	weightVulnSecurity        = 0.06
)

// Target ratios — ideal thresholds for normalization.
const (
	targetTestRatio          = 0.3 // 30% test files is ideal
	targetDocRatio           = 0.6 // 60% documented symbols is ideal
	targetErrorHandlingRatio = 0.6 // 60% error-handling coverage is ideal
)

// Normalization parameters: target (ideal) and range (spread for 0→1 mapping).
// Sub-score formula: clamp01(1.0 - (metric - target) / normRange)
const (
	targetCognitiveComplexity = 5.0
	rangeCognitiveComplexity  = 20.0

	targetCyclomaticAvg = 3.0
	rangeCyclomaticAvg  = 12.0

	targetCyclomaticMax = 8.0
	rangeCyclomaticMax  = 17.0

	targetFuncSize = 15.0
	rangeFuncSize  = 45.0

	targetNestingDepth = 2.0
	rangeNestingDepth  = 5.0

	// Multipliers for ratio-based scores (score = 1 - ratio * multiplier).
	fileSizeMultiplier    = 2.0
	duplicationMultiplier = 5.0
	magicNumberMultiplier = 3.0
	semanticDupMultiplier = 4.0

	// Target for dependency freshness ratio.
	targetDepFreshness = 0.8

	// Target for vulnerability security ratio.
	targetVulnSecurity = 1.0
)

// GradeScore computes a quality score in [0, 100] from RepoMetrics.
// Higher is better. Uses 14 sub-scores with weights summing to 1.0.
func GradeScore(m RepoMetrics) float64 {
	if m.Files == 0 {
		return 0
	}

	// Each sub-score is in [0, 1], where 1 = best.
	cognitiveScore := clamp01(1.0 - (m.AvgCognitiveComplexity-targetCognitiveComplexity)/rangeCognitiveComplexity)
	cyclomaticAvgScore := clamp01(1.0 - (m.AvgComplexity-targetCyclomaticAvg)/rangeCyclomaticAvg)
	cyclomaticMaxScore := clamp01(1.0 - (float64(m.MaxComplexity)-targetCyclomaticMax)/rangeCyclomaticMax)
	testScore := clamp01(m.TestRatio / targetTestRatio)
	docScore := clamp01(m.DocRatio / targetDocRatio)
	funcSizeScore := clamp01(1.0 - (m.AvgFuncLines-targetFuncSize)/rangeFuncSize)
	errorScore := clamp01(m.ErrorHandlingRatio / targetErrorHandlingRatio)
	nestingScore := clamp01(1.0 - (float64(m.MaxNestingDepth)-targetNestingDepth)/rangeNestingDepth)
	fileSizeScore := clamp01(1.0 - m.LargeFileRatio*fileSizeMultiplier)
	duplicationScore := clamp01(1.0 - m.DuplicationRatio*duplicationMultiplier)
	magicScore := clamp01(1.0 - m.MagicNumberRatio*magicNumberMultiplier)
	semanticDupScore := clamp01(1.0 - m.SemanticDupRatio*semanticDupMultiplier)
	freshnessScore := clamp01(m.DepFreshnessRatio / targetDepFreshness)
	vulnScore := clamp01(m.VulnSecurityRatio / targetVulnSecurity)

	total := cognitiveScore*weightCognitiveComplexity +
		cyclomaticAvgScore*weightCyclomaticAvg +
		cyclomaticMaxScore*weightCyclomaticMax +
		testScore*weightTestCoverage +
		docScore*weightDocCoverage +
		funcSizeScore*weightFuncSize +
		errorScore*weightErrorHandling +
		nestingScore*weightNestingDepth +
		fileSizeScore*weightFileSize +
		duplicationScore*weightDuplication +
		magicScore*weightMagicNumbers +
		semanticDupScore*weightSemanticDup +
		freshnessScore*weightDepFreshness +
		vulnScore*weightVulnSecurity

	// Dead code penalty: each confirmed dead function costs 0.4 score points, max -8.
	// Penalises repos with many unused functions regardless of other metrics.
	if m.DeadCodeCandidates > 0 {
		deadCodePenalty := math.Min(8.0, float64(m.DeadCodeCandidates)*0.4)
		return math.Round(math.Max(0, total*percentScale-deadCodePenalty))
	}
	return math.Round(total * percentScale)
}

// ComputeGrade returns a letter grade (A+ through F) for the given metrics.
func ComputeGrade(m RepoMetrics) string {
	score := GradeScore(m)
	switch {
	case score >= gradeAPlusThreshold:
		return "A+"
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
