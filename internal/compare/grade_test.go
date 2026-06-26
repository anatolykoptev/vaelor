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
		TotalDeps:              10, // >0 so freshness/vuln ratios are applied
		DepsScanned:            true,
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
		DepsScanned:            true,               // scan was run
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
		// TotalDeps intentionally 0 (zero value) — simulates a scanned repo with no manifests.
		// DepFreshnessRatio and VulnSecurityRatio are also 0 (zero value).
		DepsScanned: true, // scan WAS attempted; TotalDeps==0 means nothing found — N/A, not a penalty.
	}
	got := GradeScore(zeroDepPerfect)
	if got != 100 {
		t.Fatalf("GradeScore(zero-dep perfect) = %.0f, want 100 — zero-dep repos must not lose points for N/A dimensions", got)
	}
}

// TestGradeScore_DepsScannedGuard_ExploreShape verifies that when DepsScanned==false
// (the explore path — dep fields left at zero by design), the legacy penalty still
// applies. The score must be LOWER than a DepsScanned=true zero-dep repo.
// This test goes RED if the DepsScanned guard is removed or incorrectly inverted.
func TestGradeScore_DepsScannedGuard_ExploreShape(t *testing.T) {
	// Explore shape: DepsScanned=false (zero value), all dep fields = 0.
	// Must behave like the old code (penalty applied).
	exploreShape := RepoMetrics{
		Files:                  10,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
		// DepsScanned intentionally false (zero value) — explore does not run freshness scan.
		// DepFreshnessRatio=0, VulnSecurityRatio=0, TotalDeps=0.
	}
	got := GradeScore(exploreShape)
	// When DepsScanned==false and dep ratios==0, the legacy penalty applies:
	// both dep sub-scores are 0, costing 0.06+0.06=0.12 of the total weight.
	// Max score = 100 - 12 = 88. Must be strictly less than 100.
	if got >= 100 {
		t.Fatalf("GradeScore(explore-shape, DepsScanned=false) = %.0f, want < 100 "+
			"— explore unscanned path must NOT receive N/A neutral bonus", got)
	}
}

// TestGradeScore_DepsScannedGuard_CodeHealthShape verifies that when DepsScanned==true
// and TotalDeps==0 (scanned but no manifests found), the score is neutral — same as
// if both dep sub-scores were 1.0.
// This test goes RED if the DepsScanned guard is removed or incorrectly inverted.
func TestGradeScore_DepsScannedGuard_CodeHealthShape(t *testing.T) {
	// code_health shape: DepsScanned=true, TotalDeps=0 (no manifests found after scan).
	codeHealthShape := RepoMetrics{
		Files:                  10,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
		DepsScanned:            true, // scan ran, found nothing
		// TotalDeps=0 (zero value), DepFreshnessRatio=0 (zero value).
	}
	got := GradeScore(codeHealthShape)
	// With all other dims at target and dep dims N/A (neutral=1.0), score must be 100.
	if got != 100 {
		t.Fatalf("GradeScore(code_health-shape, DepsScanned=true, TotalDeps=0) = %.0f, want 100 "+
			"— scanned zero-dep repo must not lose points for N/A dimensions", got)
	}
}

// TestGradeScore_DepsScannedGuard_ExploreVsCodeHealth confirms that the explore shape
// scores LOWER than the code_health zero-dep shape (12 pts difference from dep weights).
func TestGradeScore_DepsScannedGuard_ExploreVsCodeHealth(t *testing.T) {
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

	exploreScore := GradeScore(base) // DepsScanned=false

	scanned := base
	scanned.DepsScanned = true // TotalDeps=0 still
	scannedScore := GradeScore(scanned)

	// The diff must be ~12 pts (0.12 of 100 from depFreshness+vuln weights).
	diff := scannedScore - exploreScore
	if diff < 10 || diff > 14 {
		t.Fatalf("score diff (scanned-zero-dep vs explore-unscanned) = %.0f, want ~12 "+
			"— DepsScanned guard must add exactly depFreshness+vuln weight recovery", diff)
	}
}
