package compare

import (
	"math"
	"testing"
)

const weightEpsilon = 1e-10

func TestWeightsSumToOne(t *testing.T) {
	sum := weightCognitiveComplexity +
		weightCyclomaticAvg +
		weightCyclomaticMax +
		weightTestCoverage +
		weightDocCoverage +
		weightFuncSize +
		weightErrorHandling +
		weightNestingDepth +
		weightFileSize +
		weightDuplication +
		weightMagicNumbers +
		weightSemanticDup +
		weightDepFreshness

	if math.Abs(sum-1.0) > weightEpsilon {
		t.Fatalf("weights sum to %.10f, want 1.0", sum)
	}
}

func TestGradeScore_ZeroFiles(t *testing.T) {
	m := RepoMetrics{Files: 0}
	if score := GradeScore(m); score != 0 {
		t.Fatalf("GradeScore(zero files) = %f, want 0", score)
	}
}

func TestGradeScore_PerfectMetrics(t *testing.T) {
	m := RepoMetrics{
		Files:                  10,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
		LargeFileRatio:         0,
		DuplicationRatio:       0,
		MagicNumberRatio:       0,
		SemanticDupRatio:       0,
		DepFreshnessRatio:      targetDepFreshness,
	}
	score := GradeScore(m)
	if score != 100 {
		t.Fatalf("GradeScore(perfect) = %f, want 100", score)
	}
}

func TestGradeScore_DepFreshnessImpact(t *testing.T) {
	base := RepoMetrics{
		Files:                  10,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
	}

	// With no dep freshness data (0.0), score should be lower.
	scoreNoDeps := GradeScore(base)

	// With perfect freshness.
	withDeps := base
	withDeps.DepFreshnessRatio = 1.0
	scoreWithDeps := GradeScore(withDeps)

	diff := scoreWithDeps - scoreNoDeps
	// Expected impact: 0.06 * 100 = 6 points (clamped at target 0.8, so 1.0/0.8 -> 1.0).
	if diff < 5 || diff > 7 {
		t.Fatalf("dep freshness impact = %.1f, want ~6", diff)
	}
}

