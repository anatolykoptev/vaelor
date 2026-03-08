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
			name: "excellent repo — A+",
			metrics: RepoMetrics{
				Files: 50, TotalLines: 5000,
				AvgFuncLines: 12, MaxFuncLines: 40,
				AvgComplexity: 3.0, MaxComplexity: 8,
				TestRatio: 0.4, DocRatio: 0.8,
				ErrorHandlingRatio: 0.7, Interfaces: 5, ExternalDeps: 10,
				// New metrics at zero → optimal scores.
			},
			expect: "A+",
		},
		{
			name: "good repo with moderate new metrics — A",
			metrics: RepoMetrics{
				Files: 50, TotalLines: 5000,
				AvgFuncLines: 15, MaxFuncLines: 50,
				AvgComplexity: 4.0, MaxComplexity: 10,
				TestRatio: 0.3, DocRatio: 0.6,
				ErrorHandlingRatio: 0.6,
				// New metrics: noticeable but not severe issues.
				AvgCognitiveComplexity: 10.0, MaxCognitiveComplexity: 20,
				MaxNestingDepth: 5, LargeFileRatio: 0.15, DuplicationRatio: 0.1,
			},
			expect: "A",
		},
		{
			name: "good repo — B",
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
			name: "mediocre repo — C",
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
				MagicNumberRatio: 0.5,
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
				score := GradeScore(tt.metrics)
				t.Errorf("ComputeGrade() = %q (score=%.0f), want %q", got, score, tt.expect)
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
		t.Errorf("GradeScore() = %.1f, want [0, 100]", score)
	}
}

func TestGradeAPlusThreshold(t *testing.T) {
	// A repo with all optimal metrics should get A+ (score >= 90).
	m := RepoMetrics{
		Files: 100, TotalLines: 10000,
		AvgFuncLines: 10, MaxFuncLines: 30,
		AvgComplexity: 2.0, MaxComplexity: 5,
		TestRatio: 0.35, DocRatio: 0.8,
		ErrorHandlingRatio: 0.7,
		AvgCognitiveComplexity: 2.0, MaxCognitiveComplexity: 8,
		MaxNestingDepth: 2, LargeFileRatio: 0.0, DuplicationRatio: 0.0,
	}
	grade := ComputeGrade(m)
	if grade != "A+" {
		score := GradeScore(m)
		t.Errorf("optimal metrics: grade=%q (score=%.0f), want A+", grade, score)
	}
}

func TestNewMetricsAffectScore(t *testing.T) {
	base := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6,
	}
	baseScore := GradeScore(base)

	// Adding high cognitive complexity should lower the score.
	withHighCognitive := base
	withHighCognitive.AvgCognitiveComplexity = 20.0
	cogScore := GradeScore(withHighCognitive)
	if cogScore >= baseScore {
		t.Errorf("high cognitive complexity: score=%.0f >= base=%.0f", cogScore, baseScore)
	}

	// Adding deep nesting should lower the score.
	withDeepNesting := base
	withDeepNesting.MaxNestingDepth = 8
	nestScore := GradeScore(withDeepNesting)
	if nestScore >= baseScore {
		t.Errorf("deep nesting: score=%.0f >= base=%.0f", nestScore, baseScore)
	}

	// Adding large file ratio should lower the score.
	withLargeFiles := base
	withLargeFiles.LargeFileRatio = 0.5
	fileScore := GradeScore(withLargeFiles)
	if fileScore >= baseScore {
		t.Errorf("large file ratio: score=%.0f >= base=%.0f", fileScore, baseScore)
	}

	// Adding duplication should lower the score.
	withDups := base
	withDups.DuplicationRatio = 0.3
	dupScore := GradeScore(withDups)
	if dupScore >= baseScore {
		t.Errorf("high duplication: score=%.0f >= base=%.0f", dupScore, baseScore)
	}
}

func TestSemanticDupAffectsScore(t *testing.T) {
	base := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6,
	}
	baseScore := GradeScore(base)

	withSemDup := base
	withSemDup.SemanticDupRatio = 0.4
	semScore := GradeScore(withSemDup)

	if semScore >= baseScore {
		t.Errorf("semantic dup ratio 0.4: score=%.0f >= base=%.0f", semScore, baseScore)
	}
}
