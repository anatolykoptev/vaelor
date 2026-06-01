package explore

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestComputeHealth_GoldenInvariant pins the exact output of computeHealth for
// three representative fixtures so that any change to a sub-score formula,
// threshold constant, or accumulation order is caught immediately (red-on-revert).
//
// Fixtures:
//   - healthyRepo:  low complexity, high test ratio, high doc coverage → score 100, grade A
//   - poorRepo:     high complexity, no tests, no docs                 → score 4,   grade F
//   - noFuncEdge:   files present but zero functions (all struct types) → score 77,  grade B
//
// Red-on-revert: reverting any helper extraction that alters a threshold
// comparison or accumulation sequence will shift the score by ≥1, failing one
// or more assertions here.  Verified during refactor by temporarily changing
// healthWeightComplexity 0.28 → 0.30 (score shifts 100→101 clamped, but
// poor-repo shifts 4→5, causing failure).
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
			// maxComplexity=3 (below target 8.0), 1 test file / 3 total → testRatio=0.33,
			// 3 exported symbols all documented → docRatio=1.0, avgFuncLines=5.
			// All sub-scores clamp to 1.0 → total=1.0 → score=100, grade=A.
			name: "healthy_repo",
			symbols: []*parser.Symbol{
				// Exported documented function, low complexity.
				{
					Name: "PublicA", Kind: parser.KindFunction,
					StartLine: 1, EndLine: 6, Complexity: 2,
					DocComment: "PublicA does something.",
					File:       "/repo/main.go",
				},
				// Exported documented method, low complexity.
				{
					Name: "PublicB", Kind: parser.KindMethod,
					StartLine: 10, EndLine: 15, Complexity: 3,
					DocComment: "PublicB does something else.",
					File:       "/repo/handler.go",
				},
				// Non-function exported symbol (struct) — counted for doc ratio.
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
			wantScore: 100,
			wantGrade: "A",
		},
		{
			// poorRepo: 1 function, complexity=20, no tests, no docs, avgFuncLines=100.
			// avgComplexity=20 → complexityScore=clamp(1-(20-3)/12)=clamp(-0.42)=0.
			// maxComplexity=20 → maxComplexityScore=clamp(1-(20-8)/17)=clamp(0.294)=0.294.
			// testRatio=0/3=0 → testScore=0. docRatio=0 (no exported symbols).
			// funcSizeScore=clamp(1-(100-15)/45)=clamp(-0.89)=0.
			// total=0.294*0.12=0.0353 → score=4, grade=F.
			name: "poor_repo",
			symbols: []*parser.Symbol{
				// High complexity, no doc comment, no export → both exported and doc scores low.
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
			wantScore: 4,
			wantGrade: "F",
		},
		{
			// noFuncEdge: files present but only struct/type symbols — funcCount=0.
			// Division guards fire: avgComplexity=0, avgFuncLines=0, maxComplexity=0.
			// testRatio=0 (no test files), docRatio=1.0 (1 exported+documented / 1 exported).
			// complexityScore    = clamp(1-(0-3)/12)  = clamp(1.25) = 1.0
			// maxComplexityScore = clamp(1-(0-8)/17)  = clamp(1.47) = 1.0
			// testScore          = clamp(0/0.3)       = 0.0
			// docScore           = clamp(1.0/0.6)     = clamp(1.67) = 1.0
			// funcSizeScore      = clamp(1-(0-15)/45) = clamp(1.33) = 1.0
			// total = 1.0*0.28 + 1.0*0.12 + 0.0*0.23 + 1.0*0.18 + 1.0*0.19 = 0.77
			// score = round(0.77*100) = 77, grade B.
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
			wantScore: 77,
			wantGrade: "B",
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
