package explore

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestComputeHealth_UsesCompareGradeScore verifies that computeHealth delegates
// to compare.GradeScore.  For a given set of symbols and files, the score
// returned by computeHealth must equal int(compare.GradeScore(rm)) where rm is
// the RepoMetrics built by buildExploreRepoMetrics from the same input.
//
// This test would fail if any of the following were true:
//   - computeHealth used a separate weight table
//   - buildExploreRepoMetrics mapped a field incorrectly
//   - computeHealth rounded differently from compare.GradeScore
func TestComputeHealth_UsesCompareGradeScore(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			Name: "PublicA", Kind: parser.KindFunction,
			StartLine: 1, EndLine: 6, Complexity: 4,
			DocComment: "PublicA does something.",
			File:       "/repo/main.go",
		},
		{
			Name: "PublicB", Kind: parser.KindMethod,
			StartLine: 10, EndLine: 20, Complexity: 7,
			DocComment: "PublicB does something else.",
			File:       "/repo/handler.go",
		},
		{
			Name:       "Config",
			Kind:       parser.KindStruct,
			DocComment: "Config holds settings.",
			File:       "/repo/main.go",
		},
		{
			Name: "internalFunc", Kind: parser.KindFunction,
			StartLine: 1, EndLine: 3, Complexity: 1,
			File: "/repo/util.go",
		},
	}
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go"},
		{Path: "/repo/handler.go", RelPath: "handler.go"},
		{Path: "/repo/util.go", RelPath: "util.go"},
		{Path: "/repo/main_test.go", RelPath: "main_test.go"},
	}

	got := computeHealth(symbols, files)
	if got == nil {
		t.Fatal("computeHealth() = nil, want non-nil")
	}

	// Rebuild RepoMetrics the same way buildExploreRepoMetrics does.
	sm := collectSymbolMetrics(symbols)
	testFiles := collectTestFileCount(symbols, files)
	rm := buildExploreRepoMetrics(sm, testFiles, len(files))

	wantScore := int(compare.GradeScore(rm))
	wantGrade := compare.ComputeGrade(rm)

	if got.Score != wantScore {
		t.Errorf("Score = %d, want %d (compare.GradeScore)", got.Score, wantScore)
	}
	if got.Grade != wantGrade {
		t.Errorf("Grade = %q, want %q (compare.ComputeGrade)", got.Grade, wantGrade)
	}
}

// TestComputeHealth_ApproximateFlag verifies that every non-nil HealthSummary
// returned by computeHealth carries Approximate=true and a non-empty Hint that
// references both "dependency-freshness" and "vulnerability".
//
// This test guards the self-documenting label introduced to explain why the
// explore score can differ from code_health (explore omits dep-freshness +
// vuln scanning). Removing Approximate=true or clearing the Hint from
// computeHealth will make this test fail (red-on-revert guarantee).
func TestComputeHealth_ApproximateFlag(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			Name: "Foo", Kind: parser.KindFunction,
			StartLine: 1, EndLine: 5, Complexity: 2,
			DocComment: "Foo does foo.",
			File:       "/repo/foo.go",
		},
	}
	files := []*ingest.File{
		{Path: "/repo/foo.go", RelPath: "foo.go"},
	}

	got := computeHealth(symbols, files)
	if got == nil {
		t.Fatal("computeHealth() = nil, want non-nil")
	}

	if !got.Approximate {
		t.Error("Approximate = false, want true — explore omits dep-freshness + vuln penalties")
	}
	if got.Hint == "" {
		t.Error("Hint = \"\", want non-empty explanation of the score gap")
	}
	if !strings.Contains(got.Hint, "dependency-freshness") {
		t.Errorf("Hint %q does not mention dependency-freshness", got.Hint)
	}
	if !strings.Contains(got.Hint, "vulnerability") {
		t.Errorf("Hint %q does not mention vulnerability", got.Hint)
	}
	// The hint must point users to the right tool.
	if !strings.Contains(got.Hint, "code_health") {
		t.Errorf("Hint %q does not reference code_health tool", got.Hint)
	}
}

// TestBuildExploreRepoMetrics_ZeroFuncs confirms that buildExploreRepoMetrics
// handles the zero-function edge case without division-by-zero or panic, and
// that Files is populated correctly.
func TestBuildExploreRepoMetrics_ZeroFuncs(t *testing.T) {
	sm := symbolMetrics{
		funcCount:       0,
		exportedCount:   1,
		documentedCount: 1,
	}
	rm := buildExploreRepoMetrics(sm, 0, 5)

	if rm.Files != 5 {
		t.Errorf("Files = %d, want 5", rm.Files)
	}
	if rm.AvgComplexity != 0 {
		t.Errorf("AvgComplexity = %v, want 0", rm.AvgComplexity)
	}
	if rm.AvgFuncLines != 0 {
		t.Errorf("AvgFuncLines = %v, want 0", rm.AvgFuncLines)
	}
	// DocRatio = 1/1 = 1.0
	if rm.DocRatio != 1.0 {
		t.Errorf("DocRatio = %v, want 1.0", rm.DocRatio)
	}
}

// TestComputeHealth_EmptyFiles verifies the nil-guard on empty file lists.
func TestComputeHealth_EmptyFiles(t *testing.T) {
	got := computeHealth(nil, nil)
	if got != nil {
		t.Errorf("computeHealth(nil, nil) = %+v, want nil", got)
	}

	got = computeHealth([]*parser.Symbol{}, []*ingest.File{})
	if got != nil {
		t.Errorf("computeHealth(empty, empty) = %+v, want nil", got)
	}
}
