package compare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
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
	}, nil) // nil LLM = skip LLM analysis
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
