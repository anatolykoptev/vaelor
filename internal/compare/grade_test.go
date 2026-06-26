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
		weightDepFreshness +
		weightVulnSecurity

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
		VulnSecurityRatio:      targetVulnSecurity,
		TotalDeps:              10, // must be >0 so freshness/vuln ratios are applied
	}
	score := GradeScore(m)
	if score != 100 {
		t.Fatalf("GradeScore(perfect) = %f, want 100", score)
	}
}

func TestGradeScore_DepFreshnessImpact(t *testing.T) {
	// base has TotalDeps>0 so the freshness dimension is active (not N/A).
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
		TotalDeps:              5,                  // >0 activates freshness/vuln scoring
		VulnSecurityRatio:      targetVulnSecurity, // vuln perfect so only freshness varies
	}

	// With zero DepFreshnessRatio (all deps outdated), score should be penalised.
	base.DepFreshnessRatio = 0.0
	scoreAllOutdated := GradeScore(base)

	// With perfect freshness (all deps current).
	withGoodDeps := base
	withGoodDeps.DepFreshnessRatio = 1.0
	scoreGoodDeps := GradeScore(withGoodDeps)

	diff := scoreGoodDeps - scoreAllOutdated
	// Expected impact: weightDepFreshness * 100 = 0.06 * 100 = 6 points.
	if diff < 5 || diff > 7 {
		t.Fatalf("dep freshness impact = %.1f, want ~6 (TotalDeps>0 path)", diff)
	}
}

// TestGradeScore_ZeroDepsNeutral verifies that a repo with no dependency manifests
// (TotalDeps==0) does not lose any points for freshness or vulnerability scoring.
// A stdlib-only repo should reach 100 on all other perfect dims.
func TestGradeScore_ZeroDepsNeutral(t *testing.T) {
	// All quality dims at target, TotalDeps==0 (no manifests found).
	zeroDepPerfect := RepoMetrics{
		Files:                  10,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
		// TotalDeps intentionally 0 (zero value) — simulates no go.mod/package.json found.
		// DepFreshnessRatio and VulnSecurityRatio are also 0 (zero value).
	}
	got := GradeScore(zeroDepPerfect)
	if got != 100 {
		t.Fatalf("GradeScore(zero-dep perfect) = %.0f, want 100 — zero-dep repos must not lose points for N/A dimensions", got)
	}
}
