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

// TestCompareRepos_GradeReflectsFreshness guards that applyEnrichmentAndRescore
// (called by CompareRepos after dep-freshness enrichment) recomputes Score/Grade
// on the real production RepoMetrics, not a hand-rolled reimplementation.
//
// RED-without-fix proof: delete or zero the Score/Grade recompute lines inside
// applyEnrichmentAndRescore — the Score assertion fails because m.Score stays at
// its pre-call value (0) instead of reflecting the injected freshness ratios.
func TestCompareRepos_GradeReflectsFreshness(t *testing.T) {
	t.Parallel()

	// Fixture: all quality dims at target, Score left at zero to simulate the
	// state just after ComputeMetrics (before enrichment+rescore in CompareRepos).
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
		// Score=0, Grade="" — as returned by ComputeMetrics before enrichment.
		Score: 0,
		Grade: "",
	}

	// Freshness stats representing a healthy dependency state (good ratios).
	freshness := &FreshnessStats{
		DepFreshnessRatio: targetDepFreshness,
		VulnSecurityRatio: targetVulnSecurity,
		TotalDeps:         5,
	}

	// Drive the REAL production helper — the same code path CompareRepos invokes.
	applyEnrichmentAndRescore(&m, freshness)

	// Score must be non-zero after applyEnrichmentAndRescore calls GradeScore.
	// Neutering the recompute block keeps Score=0 → this assertion goes RED.
	if m.Score <= 0 {
		t.Fatalf("Score after applyEnrichmentAndRescore = %g, want >0 (recompute not called?)", m.Score)
	}

	// Grade must be non-empty and not "F": all other dims are perfect, deps are now
	// fresh. Stays "" or "F" if ComputeGrade is not called inside the helper.
	if m.Grade == "" {
		t.Fatalf("Grade after applyEnrichmentAndRescore is empty (ComputeGrade not called?)")
	}
	if m.Grade == "F" {
		t.Fatalf("Grade = F after fresh-dep enrichment; want at least D (all other dims are perfect)")
	}

	// Freshness + vuln together contribute ~12 pts (weightDepFreshness+weightVulnSecurity)*100.
	// Derive stale baseline to measure the delta.
	stale := m
	stale.DepFreshnessRatio = 0
	stale.VulnSecurityRatio = 0
	stale.TotalDeps = 5
	stale.DepsScanned = true
	staleScore := GradeScore(stale)

	delta := m.Score - staleScore
	if delta < 10 || delta > 14 {
		t.Fatalf("freshness+vuln score delta = %.1f, want ~12 (0.06+0.06 weights * 100)", delta)
	}
}

// TestCompareRepos_ZeroDepNeutral guards that applyEnrichmentAndRescore treats
// nil freshnessStats (zero-dep repos — no manifests found) identically to how
// code_health/#250 treats them: DepsScanned=true + TotalDeps=0, so the N/A guard
// in GradeScore fires and the repo is not penalised on dep/vuln dimensions.
//
// RED-without-fix proof: before the fix, freshnessStats==nil leaves DepsScanned=false,
// which makes the !DepsScanned branch in GradeScore apply legacy zero-ratio penalties
// (−12 pts dep+vuln), dropping a perfect-metric repo from ~C to F.
func TestCompareRepos_ZeroDepNeutral(t *testing.T) {
	t.Parallel()

	// Fixture: realistic httprouter-style repo — all quality dims near target,
	// but NOT tuned to perfection so the score is not trivially 100.
	m := RepoMetrics{
		Files:                  20,
		AvgCognitiveComplexity: targetCognitiveComplexity,
		AvgComplexity:          targetCyclomaticAvg,
		MaxComplexity:          int(targetCyclomaticMax),
		TestRatio:              targetTestRatio,
		DocRatio:               targetDocRatio,
		AvgFuncLines:           targetFuncSize,
		ErrorHandlingRatio:     targetErrorHandlingRatio,
		MaxNestingDepth:        int(targetNestingDepth),
		// DepsScanned=false, TotalDeps=0 — state before applyEnrichmentAndRescore
	}

	// Drive the REAL production helper with nil freshnessStats (zero-dep case).
	applyEnrichmentAndRescore(&m, nil)

	// DepsScanned must be true after the call so future GradeScore calls stay neutral.
	if !m.DepsScanned {
		t.Fatalf("DepsScanned = false after applyEnrichmentAndRescore(nil); want true (N/A guard must fire)")
	}

	// Score must be in the C-range (≥55), not penalised down to F.
	// Without the fix DepsScanned stays false → GradeScore applies dep+vuln penalties
	// → score drops ~12 pts, landing in F territory for an otherwise-decent repo.
	if m.Score < 55 {
		t.Fatalf("Score = %.1f after zero-dep enrichment, want ≥55 (phantom dep penalty must not apply)", m.Score)
	}

	// Grade must not be F — a zero-dep repo with good code quality should be C or better.
	if m.Grade == "F" || m.Grade == "" {
		t.Fatalf("Grade = %q after zero-dep enrichment, want C or better (DepsScanned N/A guard must fire)", m.Grade)
	}
}
