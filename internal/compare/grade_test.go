package compare

import (
	"testing"
)

func TestComputeGrade(t *testing.T) {
	tests := []struct {
		name    string
		metrics RepoMetrics
		expect  string
	}{
		{
			name: "excellent repo",
			metrics: RepoMetrics{
				Files: 50, TotalLines: 5000,
				AvgFuncLines: 12, MaxFuncLines: 40,
				AvgComplexity: 3.0, MaxComplexity: 8,
				TestRatio: 0.4, DocRatio: 0.8,
				ErrorHandlingRatio: 0.7, Interfaces: 5, ExternalDeps: 10,
			},
			expect: "A",
		},
		{
			name: "good repo",
			metrics: RepoMetrics{
				Files: 30, TotalLines: 4000,
				AvgFuncLines: 25, MaxFuncLines: 60,
				AvgComplexity: 6.0, MaxComplexity: 15,
				TestRatio: 0.15, DocRatio: 0.35,
				ErrorHandlingRatio: 0.4, Interfaces: 3, ExternalDeps: 15,
			},
			expect: "B",
		},
		{
			name: "mediocre repo",
			metrics: RepoMetrics{
				Files: 20, TotalLines: 3000,
				AvgFuncLines: 35, MaxFuncLines: 90,
				AvgComplexity: 8.0, MaxComplexity: 18,
				TestRatio: 0.1, DocRatio: 0.2,
				ErrorHandlingRatio: 0.3, Interfaces: 1, ExternalDeps: 25,
			},
			expect: "C",
		},
		{
			name: "poor repo — high complexity, few tests, no docs",
			metrics: RepoMetrics{
				Files: 10, TotalLines: 2000,
				AvgFuncLines: 45, MaxFuncLines: 150,
				AvgComplexity: 10.0, MaxComplexity: 20,
				TestRatio: 0.05, DocRatio: 0.1,
				ErrorHandlingRatio: 0.2, Interfaces: 0, ExternalDeps: 30,
			},
			expect: "D",
		},
		{
			name:    "empty repo",
			metrics: RepoMetrics{},
			expect:  "F",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeGrade(tt.metrics)
			if got != tt.expect {
				t.Errorf("ComputeGrade() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGradeScoreRange(t *testing.T) {
	metrics := RepoMetrics{
		Files: 100, TotalLines: 10000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6, Interfaces: 8, ExternalDeps: 12,
	}
	score := GradeScore(metrics)
	if score < 0 || score > 100 {
		t.Errorf("gradeScore() = %.1f, want [0, 100]", score)
	}
}
