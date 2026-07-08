package compare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-kit/llm"
)

// findRepoRootInternal walks upward from the current working directory until
// it finds a directory containing go.mod, which is the repository root.
// This variant is for internal (package compare) tests only.
func findRepoRootInternal(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found — cannot locate repo root")
		}
		dir = parent
	}
}

func TestSymbolMatch_IsGap(t *testing.T) {
	tests := []struct {
		name   string
		match  SymbolMatch
		expect bool
	}{
		{
			name: "both symbols present — not a gap",
			match: SymbolMatch{
				SymbolA: &parser.Symbol{Name: "Foo"},
				SymbolB: &parser.Symbol{Name: "Foo"},
			},
			expect: false,
		},
		{
			name: "only A present — gap",
			match: SymbolMatch{
				SymbolA: &parser.Symbol{Name: "Foo"},
			},
			expect: true,
		},
		{
			name: "only B present — gap",
			match: SymbolMatch{
				SymbolB: &parser.Symbol{Name: "Bar"},
			},
			expect: true,
		},
		{
			name:   "neither present — gap",
			match:  SymbolMatch{},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.match.IsGap()
			if got != tt.expect {
				t.Errorf("IsGap() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestSymbolMatch_MissingIn(t *testing.T) {
	tests := []struct {
		name   string
		match  SymbolMatch
		expect string
	}{
		{
			name: "missing in repo_b",
			match: SymbolMatch{
				SymbolA: &parser.Symbol{Name: "Foo"},
			},
			expect: "repo_b",
		},
		{
			name: "missing in repo_a",
			match: SymbolMatch{
				SymbolB: &parser.Symbol{Name: "Bar"},
			},
			expect: "repo_a",
		},
		{
			name: "both present — empty",
			match: SymbolMatch{
				SymbolA: &parser.Symbol{Name: "Foo"},
				SymbolB: &parser.Symbol{Name: "Foo"},
			},
			expect: "",
		},
		{
			name:   "both nil — empty",
			match:  SymbolMatch{},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.match.MissingIn()
			if got != tt.expect {
				t.Errorf("MissingIn() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestRepoMetrics_ZeroValue(t *testing.T) {
	var m RepoMetrics

	if m.Files != 0 {
		t.Errorf("Files = %d, want 0", m.Files)
	}
	if m.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", m.TotalLines)
	}
	if m.AvgFuncLines != 0 {
		t.Errorf("AvgFuncLines = %f, want 0", m.AvgFuncLines)
	}
	if m.MaxFuncLines != 0 {
		t.Errorf("MaxFuncLines = %d, want 0", m.MaxFuncLines)
	}
	if m.TestRatio != 0 {
		t.Errorf("TestRatio = %f, want 0", m.TestRatio)
	}
	if m.DocRatio != 0 {
		t.Errorf("DocRatio = %f, want 0", m.DocRatio)
	}
	if m.ErrorHandlingRatio != 0 {
		t.Errorf("ErrorHandlingRatio = %f, want 0", m.ErrorHandlingRatio)
	}
	if m.Interfaces != 0 {
		t.Errorf("Interfaces = %d, want 0", m.Interfaces)
	}
	if m.ExternalDeps != 0 {
		t.Errorf("ExternalDeps = %d, want 0", m.ExternalDeps)
	}
}

func TestCompareReposIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	// Compare the repo against itself — should get exact matches, no gaps.
	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "compare error handling",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{}) // NoOp returns ErrLLMUnavailable; runLLMAnalysis treats it as a fallback
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}
	if result.MatchedSymbols == 0 {
		t.Error("expected matched symbols > 0")
	}
	if result.UnmatchedA != 0 || result.UnmatchedB != 0 {
		t.Errorf("self-compare should have 0 unmatched, got A=%d B=%d", result.UnmatchedA, result.UnmatchedB)
	}
	if result.MetricsA.Files == 0 {
		t.Error("expected metrics to be computed")
	}
}

func TestCompareRepos_MatchBreakdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "test",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{}) // NoOp returns ErrLLMUnavailable; runLLMAnalysis uses deterministic fallback
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}

	if result.MatchBreakdown.Exact == 0 {
		t.Error("expected Exact > 0 in self-compare")
	}
	if result.MatchBreakdown.Modified != 0 {
		t.Errorf("expected Modified = 0 in self-compare, got %d", result.MatchBreakdown.Modified)
	}
	if result.MatchBreakdown.Renamed != 0 {
		t.Errorf("expected Renamed = 0 in self-compare, got %d", result.MatchBreakdown.Renamed)
	}
}

func TestCompareRepos_ImportDiff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "test",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{}) // NoOp returns ErrLLMUnavailable; runLLMAnalysis uses deterministic fallback
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}

	if result.ImportDiff.CommonCount == 0 {
		t.Error("expected CommonCount > 0 in self-compare")
	}
	if result.ImportDiff.OnlyACount != 0 {
		t.Errorf("expected OnlyACount = 0 in self-compare, got %d", result.ImportDiff.OnlyACount)
	}
	if result.ImportDiff.OnlyBCount != 0 {
		t.Errorf("expected OnlyBCount = 0 in self-compare, got %d", result.ImportDiff.OnlyBCount)
	}
}

func TestCompareRepos_Hotspots(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "test",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{}) // NoOp returns ErrLLMUnavailable; runLLMAnalysis uses deterministic fallback
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}

	t.Logf("HotspotsA: %d, HotspotsB: %d", len(result.HotspotsA), len(result.HotspotsB))
}

func TestAnnotateASTDiffs(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Foo(x int) error {\n\treturn nil\n}",
			},
			SymbolB: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Foo(x int, y string) (int, error) {\n\treturn 0, nil\n}",
			},
			MatchType: MatchModified,
			Score:     1.0,
		},
		{
			SymbolA: &parser.Symbol{
				Name: "Bar", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Bar() {}",
			},
			SymbolB: &parser.Symbol{
				Name: "Bar", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Bar() {}",
			},
			MatchType: MatchExact,
			Score:     1.0,
		},
	}

	annotateASTDiffs(matches)

	if matches[0].Diff == nil {
		t.Error("expected Diff on modified match")
	}
	if matches[0].Diff.TotalChanges == 0 {
		t.Error("expected TotalChanges > 0 on modified match")
	}
	if matches[1].Diff != nil {
		t.Error("expected nil Diff on exact match")
	}
}

func TestComputeDiffStats(t *testing.T) {
	t.Run("no diffs returns nil", func(t *testing.T) {
		matches := []SymbolMatch{
			{MatchType: MatchExact},
		}
		if got := computeDiffStats(matches); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("aggregates diffs", func(t *testing.T) {
		matches := []SymbolMatch{
			{
				Diff: &DiffSummary{
					TotalChanges: 5,
					Inserts:      2,
					Deletes:      1,
					Updates:      1,
					Moves:        1,
				},
			},
			{
				Diff: &DiffSummary{
					TotalChanges: 3,
					Inserts:      1,
					Deletes:      0,
					Updates:      2,
					Moves:        0,
				},
			},
		}
		got := computeDiffStats(matches)
		if got == nil {
			t.Fatal("expected non-nil stats")
		}
		if got.ModifiedWithDiff != 2 {
			t.Errorf("ModifiedWithDiff = %d, want 2", got.ModifiedWithDiff)
		}
		if got.TotalInserts != 3 {
			t.Errorf("TotalInserts = %d, want 3", got.TotalInserts)
		}
		if got.TotalDeletes != 1 {
			t.Errorf("TotalDeletes = %d, want 1", got.TotalDeletes)
		}
		if got.TotalUpdates != 3 {
			t.Errorf("TotalUpdates = %d, want 3", got.TotalUpdates)
		}
		if got.TotalMoves != 1 {
			t.Errorf("TotalMoves = %d, want 1", got.TotalMoves)
		}
	})
}

func TestCompareRepos_Freshness(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test-full)")
	}
	root := findRepoRootInternal(t)
	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root, RootB: root,
	}, llm.NoOp{}) // NoOp returns ErrLLMUnavailable; runLLMAnalysis uses deterministic fallback
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}
	// go-code has go.mod, so freshness should be populated.
	if result.FreshnessA == nil {
		t.Log("FreshnessA is nil — freshness check may have failed (network)")
	} else if result.FreshnessA.TotalDeps == 0 {
		t.Log("TotalDeps is 0 — no deps resolved")
	}
}

func TestParseAnalysis(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		input := `{"quality": [{"aspect": "error handling", "winner": "repo_a", "reason": "better"}], "recommendations": ["use errors.Is"]}`
		got := parseAnalysis(input)
		if len(got.Quality) != 1 {
			t.Errorf("Quality length = %d, want 1", len(got.Quality))
		}
		if len(got.Recommendations) != 1 {
			t.Errorf("Recommendations length = %d, want 1", len(got.Recommendations))
		}
	})

	t.Run("markdown wrapped JSON", func(t *testing.T) {
		input := "Here is the analysis:\n```json\n{\"recommendations\": [\"improve tests\"]}\n```\n"
		got := parseAnalysis(input)
		if len(got.Recommendations) != 1 {
			t.Errorf("Recommendations length = %d, want 1", len(got.Recommendations))
		}
	})

	t.Run("plain text fallback", func(t *testing.T) {
		input := "This repo is better because..."
		got := parseAnalysis(input)
		if len(got.Recommendations) != 1 || got.Recommendations[0] != input {
			t.Errorf("expected plain text fallback, got %v", got.Recommendations)
		}
	})
}
