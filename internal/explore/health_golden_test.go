package explore

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestComputeHealth_GoldenInvariant pins the exact output of computeHealth for
// three representative fixtures so that any change to a sub-score formula,
// threshold constant, or accumulation order is caught immediately.
//
// Scores are computed via compare.GradeScore (14-factor formula). Fields that
// explore does not track default to 0. Zero is best-case for lower-is-better
// ratios (LargeFile, Dup, Magic, SemDup → score 1.0) and worst-case for
// coverage ratios (Error, DepFreshness, Vuln → score 0), capping explore at 80.
//
// Fixtures:
//   - healthyRepo:  low complexity, high test ratio, high doc coverage → score 80, grade A
//   - poorRepo:     high complexity, no tests, no docs                 → score 40, grade F
//   - noFuncEdge:   files present but zero functions (all struct types) → score 68, grade C
//
// Score derivation (healthy_repo):
//   - Files=3, AvgComplexity=2.5, MaxComplexity=3, AvgFuncLines=5
//   - TestRatio=1/3≈0.333, DocRatio=1.0; error/freshness/vuln=0 (worst-case)
//   - All 12 non-zero-default sub-scores clamp to 1.0 except error(0) and freshness(0) and vuln(0)
//   - total = 0.12+0.07+0.05+0.12+0.09+0.08+0+0.07+0.07+0.05+0.06+0.02+0+0 = 0.80
//   - score = round(0.80*100) = 80, grade = A
//
// Score derivation (poor_repo):
//   - Files=3, AvgComplexity=20, MaxComplexity=20, AvgFuncLines=100, TestRatio=0, DocRatio=0
//   - cognitiveScore=1.0, cyclomaticAvgScore=0, cyclomaticMaxScore≈0.294
//   - testScore=0, docScore=0, funcSizeScore=0, errorScore=0
//   - nestingScore=1.0, fileSizeScore=1.0, dupScore=1.0, magicScore=1.0, semDupScore=1.0
//   - total ≈ 0.12+0+0.0147+0+0+0+0+0.07+0.07+0.05+0.06+0.02 = 0.4047
//   - score = round(40.47) = 40, grade = F
//
// Score derivation (no_func_edge):
//   - Files=1, all func metrics=0, TestRatio=0, DocRatio=1.0
//   - Most sub-scores=1.0 except testScore=0, errorScore=0, freshness=0, vuln=0
//   - total = 0.12+0.07+0.05+0+0.09+0.08+0+0.07+0.07+0.05+0.06+0.02+0+0 = 0.68
//   - score = round(68) = 68, grade = C
func TestComputeHealth_GoldenInvariant(t *testing.T) {
	tests := []struct {
		name      string
		symbols   []*parser.Symbol
		files     []*ingest.File
		wantScore int
		wantGrade string
		wantNil   bool
	}{
		{
			name:    "nil_on_empty_files",
			symbols: nil,
			files:   nil,
			wantNil: true,
		},
		{
			// healthyRepo: 2 functions, avgComplexity=(2+3)/2=2.5 (below target 3.0),
			// maxComplexity=3 (below target 8.0), 1 test file / 3 total → testRatio=0.333,
			// 3 exported symbols all documented → docRatio=1.0, avgFuncLines=5.
			name: "healthy_repo",
			symbols: []*parser.Symbol{
				{
					Name: "PublicA", Kind: parser.KindFunction,
					StartLine: 1, EndLine: 6, Complexity: 2,
					DocComment: "PublicA does something.",
					File:       "/repo/main.go",
				},
				{
					Name: "PublicB", Kind: parser.KindMethod,
					StartLine: 10, EndLine: 15, Complexity: 3,
					DocComment: "PublicB does something else.",
					File:       "/repo/handler.go",
				},
				{
					Name:       "Config",
					Kind:       parser.KindStruct,
					DocComment: "Config holds settings.",
					File:       "/repo/main.go",
				},
			},
			files: []*ingest.File{
				{Path: "/repo/main.go", RelPath: "main.go"},
				{Path: "/repo/handler.go", RelPath: "handler.go"},
				{Path: "/repo/main_test.go", RelPath: "main_test.go"},
			},
			wantScore: 80,
			wantGrade: "A",
		},
		{
			// poorRepo: 1 function, complexity=20, no tests, no docs, avgFuncLines=100.
			// compare.GradeScore gives score=40, grade=F.
			name: "poor_repo",
			symbols: []*parser.Symbol{
				{
					Name: "bigFunc", Kind: parser.KindFunction,
					StartLine: 1, EndLine: 101, Complexity: 20,
					DocComment: "",
					File:       "/repo/big.go",
				},
			},
			files: []*ingest.File{
				{Path: "/repo/big.go", RelPath: "big.go"},
				{Path: "/repo/other.go", RelPath: "other.go"},
				{Path: "/repo/more.go", RelPath: "more.go"},
			},
			wantScore: 40,
			wantGrade: "F",
		},
		{
			// noFuncEdge: files present but only struct/type symbols — funcCount=0.
			// compare.GradeScore gives score=68, grade=C.
			name: "no_func_edge",
			symbols: []*parser.Symbol{
				{
					Name:       "MyStruct",
					Kind:       parser.KindStruct,
					DocComment: "MyStruct is a thing.",
					File:       "/repo/types.go",
				},
			},
			files: []*ingest.File{
				{Path: "/repo/types.go", RelPath: "types.go"},
			},
			wantScore: 68,
			wantGrade: "C",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeHealth(tc.symbols, tc.files)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("computeHealth() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("computeHealth() = nil, want non-nil")
			}
			if got.Score != tc.wantScore {
				t.Errorf("Score = %d, want %d", got.Score, tc.wantScore)
			}
			if got.Grade != tc.wantGrade {
				t.Errorf("Grade = %q, want %q", got.Grade, tc.wantGrade)
			}
		})
	}
}
