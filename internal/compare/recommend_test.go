package compare

import (
	"math"
	"testing"
)

// TestSubScoreSumMatchesGradeScore guards against drift between
// computeSubScores (in recommend.go) and GradeScore (in grade.go).
func TestSubScoreSumMatchesGradeScore(t *testing.T) {
	cases := []RepoMetrics{
		{Files: 50, TotalLines: 5000, AvgFuncLines: 15, AvgComplexity: 4, MaxComplexity: 10,
			TestRatio: 0.3, DocRatio: 0.6, ErrorHandlingRatio: 0.6},
		{Files: 10, TotalLines: 1000, AvgFuncLines: 40, AvgComplexity: 9, MaxComplexity: 20,
			TestRatio: 0.05, DocRatio: 0.1, ErrorHandlingRatio: 0.2,
			AvgCognitiveComplexity: 15, MaxNestingDepth: 6, LargeFileRatio: 0.3, DuplicationRatio: 0.2},
		{Files: 100, TotalLines: 10000, AvgFuncLines: 10, AvgComplexity: 2, MaxComplexity: 5,
			TestRatio: 0.35, DocRatio: 0.8, ErrorHandlingRatio: 0.7,
			AvgCognitiveComplexity: 2, MaxNestingDepth: 2},
	}

	for i, m := range cases {
		got := SubScoreSum(m)
		want := GradeScore(m)
		if math.Abs(got-want) > 0.5 {
			t.Errorf("case %d: SubScoreSum=%.0f, GradeScore=%.0f — drift detected", i, got, want)
		}
	}
}

func TestSubScoreSumEmpty(t *testing.T) {
	if got := SubScoreSum(RepoMetrics{}); got != 0 {
		t.Errorf("SubScoreSum(empty) = %.0f, want 0", got)
	}
}

func TestComputeRecommendations_Sorting(t *testing.T) {
	m := RepoMetrics{
		Files: 20, TotalLines: 2000,
		AvgFuncLines: 35, AvgComplexity: 8, MaxComplexity: 20,
		TestRatio: 0.05, DocRatio: 0.1,
		ErrorHandlingRatio: 0.2,
		AvgCognitiveComplexity: 18, MaxNestingDepth: 7,
		LargeFileRatio: 0.4, DuplicationRatio: 0.3,
	}
	recs := ComputeRecommendations(m, Outliers{}, 0)

	if len(recs) == 0 {
		t.Fatal("expected recommendations for poor metrics")
	}

	for i := 1; i < len(recs); i++ {
		if recs[i].Potential > recs[i-1].Potential {
			t.Errorf("recs not sorted by potential: [%d]=%d > [%d]=%d",
				i, recs[i].Potential, i-1, recs[i-1].Potential)
		}
	}

	for i, r := range recs {
		if r.Priority != i+1 {
			t.Errorf("rec[%d].Priority = %d, want %d", i, r.Priority, i+1)
		}
	}
}

func TestComputeRecommendations_MaxItems(t *testing.T) {
	m := RepoMetrics{
		Files: 20, TotalLines: 2000,
		AvgFuncLines: 35, AvgComplexity: 8, MaxComplexity: 20,
		TestRatio: 0.05, DocRatio: 0.1,
		ErrorHandlingRatio: 0.2,
		AvgCognitiveComplexity: 18, MaxNestingDepth: 7,
	}
	recs := ComputeRecommendations(m, Outliers{}, 3)
	if len(recs) > 3 {
		t.Errorf("got %d recs, want ≤3", len(recs))
	}
}

func TestComputeRecommendations_PerfectMetrics(t *testing.T) {
	m := RepoMetrics{
		Files: 100, TotalLines: 10000,
		AvgFuncLines: 10, AvgComplexity: 2, MaxComplexity: 5,
		TestRatio: 0.35, DocRatio: 0.7, ErrorHandlingRatio: 0.7,
		AvgCognitiveComplexity: 3, MaxNestingDepth: 1,
	}
	recs := ComputeRecommendations(m, Outliers{}, 0)
	if len(recs) != 0 {
		t.Errorf("expected 0 recs for perfect metrics, got %d", len(recs))
	}
}

func TestComputeRecommendations_OutlierInMessage(t *testing.T) {
	m := RepoMetrics{
		Files: 20, TotalLines: 2000,
		AvgComplexity: 8, MaxComplexity: 25,
	}
	out := Outliers{
		MaxCyclomatic: OutlierFunc{Name: "BigFunc", File: "pkg/main.go", Line: 42, Value: 25},
	}
	recs := ComputeRecommendations(m, out, 0)

	found := false
	for _, r := range recs {
		if r.Area == "cyclomatic_max" || r.Area == "cyclomatic_avg" {
			if contains(r.Message, "BigFunc") && contains(r.Message, "pkg/main.go:42") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected outlier info in cyclomatic recommendation message")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
