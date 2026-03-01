package compare

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func makeSymbol(name, kind, file string, body string) *parser.Symbol {
	return &parser.Symbol{
		Name:      name,
		Kind:      parser.NodeKind(kind),
		File:      file,
		StartLine: 10,
		EndLine:   20,
		Body:      body,
	}
}

func TestBuildCompareContext(t *testing.T) {
	metricsA := RepoMetrics{
		AvgFuncLines: 25.5,
		MaxFuncLines: 120,
		TestRatio:    0.3,
		DocRatio:     0.8,
		ExternalDeps: 5,
	}
	metricsB := RepoMetrics{
		AvgFuncLines: 18.0,
		MaxFuncLines: 80,
		TestRatio:    0.4,
		DocRatio:     0.7,
		ExternalDeps: 3,
	}

	matches := []SymbolMatch{
		{
			SymbolA:   makeSymbol("ServeHTTP", "function", "/repo-a/server.go", "func ServeHTTP(w, r) {}"),
			SymbolB:   makeSymbol("ServeHTTP", "function", "/repo-b/server.go", "func ServeHTTP(w, r) { /* v2 */ }"),
			MatchType: MatchExact,
			Category:  "http",
			Score:     0.95,
		},
		{
			// gap: missing in repo_b
			SymbolA:   makeSymbol("OldHandler", "function", "/repo-a/old.go", "func OldHandler() {}"),
			SymbolB:   nil,
			MatchType: MatchGap,
			Category:  "http",
			Score:     0,
		},
		{
			// gap: missing in repo_a
			SymbolA:   nil,
			SymbolB:   makeSymbol("NewFeature", "function", "/repo-b/feature.go", "func NewFeature() {}"),
			MatchType: MatchGap,
			Category:  "feature",
			Score:     0,
		},
	}

	query := "Compare error handling strategies"
	result := BuildCompareContext(matches, metricsA, metricsB, query)

	t.Run("contains query", func(t *testing.T) {
		if !strings.Contains(result, query) {
			t.Errorf("expected result to contain query %q", query)
		}
	})

	t.Run("contains metrics section", func(t *testing.T) {
		if !strings.Contains(result, "## Metrics") {
			t.Error("expected result to contain ## Metrics section")
		}
		if !strings.Contains(result, "avgFuncLines") {
			t.Error("expected result to contain avgFuncLines in metrics JSON")
		}
	})

	t.Run("contains matched symbol name", func(t *testing.T) {
		if !strings.Contains(result, "ServeHTTP") {
			t.Error("expected result to contain matched symbol ServeHTTP")
		}
	})

	t.Run("contains gap info", func(t *testing.T) {
		if !strings.Contains(result, "## Coverage Gaps") {
			t.Error("expected result to contain ## Coverage Gaps section")
		}
		if !strings.Contains(result, "OldHandler") {
			t.Error("expected result to contain gap symbol OldHandler")
		}
		if !strings.Contains(result, "NewFeature") {
			t.Error("expected result to contain gap symbol NewFeature")
		}
		if !strings.Contains(result, "MISSING in repo_b") {
			t.Error("expected result to contain MISSING in repo_b")
		}
		if !strings.Contains(result, "MISSING in repo_a") {
			t.Error("expected result to contain MISSING in repo_a")
		}
	})
}

func TestBuildCompareContext_IncludesHotspots(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA:   &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "a.go", Body: "code"},
			SymbolB:   &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "b.go", Body: "code"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
	}

	hotspotsA := []HotspotFile{
		{File: "hot.go", Score: 0.95, Churn: 50, Complexity: 12.0, Risk: "critical"},
	}
	hotspotsB := []HotspotFile{
		{File: "warm.go", Score: 0.70, Churn: 30, Complexity: 8.0, Risk: "high"},
	}

	ctx := BuildCompareContextV2(matches, RepoMetrics{}, RepoMetrics{}, "test", hotspotsA, hotspotsB)

	if !strings.Contains(ctx, "Hotspots") {
		t.Error("context should contain Hotspots section")
	}
	if !strings.Contains(ctx, "hot.go") {
		t.Error("context should contain hotspot file hot.go")
	}
	if !strings.Contains(ctx, "critical") {
		t.Error("context should contain risk level critical")
	}
}

func TestBuildCompareContext_PrioritizesModified(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA:   &parser.Symbol{Name: "Identical1", Kind: parser.KindFunction, File: "a.go", Body: "same"},
			SymbolB:   &parser.Symbol{Name: "Identical1", Kind: parser.KindFunction, File: "b.go", Body: "same"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
		{
			SymbolA:   &parser.Symbol{Name: "Changed1", Kind: parser.KindFunction, File: "a.go", Body: "old code"},
			SymbolB:   &parser.Symbol{Name: "Changed1", Kind: parser.KindFunction, File: "b.go", Body: "new code"},
			MatchType: MatchModified, Score: 1.0, Category: "function",
		},
		{
			SymbolA:   &parser.Symbol{Name: "Identical2", Kind: parser.KindFunction, File: "a.go", Body: "same2"},
			SymbolB:   &parser.Symbol{Name: "Identical2", Kind: parser.KindFunction, File: "b.go", Body: "same2"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
		{
			SymbolA:   &parser.Symbol{Name: "Renamed1", Kind: parser.KindFunction, File: "a.go", Body: "code"},
			SymbolB:   &parser.Symbol{Name: "RenamedOne", Kind: parser.KindFunction, File: "b.go", Body: "code"},
			MatchType: MatchRenamed, Score: 0.9, Category: "function",
		},
	}

	ctx := BuildCompareContext(matches, RepoMetrics{}, RepoMetrics{}, "test")

	changedIdx := strings.Index(ctx, "Changed1")
	renamedIdx := strings.Index(ctx, "Renamed1")
	identical1Idx := strings.Index(ctx, "Identical1")

	if changedIdx < 0 {
		t.Fatal("Changed1 not found in context")
	}
	if renamedIdx < 0 {
		t.Fatal("Renamed1 not found in context")
	}
	if identical1Idx >= 0 && identical1Idx < changedIdx {
		t.Error("Identical1 appeared before Changed1 — modified symbols should be prioritized")
	}
}

func TestBuildCompareContext_IncludesDiffSummary(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction, File: "a.go",
				Body: "func Foo(x int) {}",
			},
			SymbolB: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction, File: "b.go",
				Body: "func Foo(x int, y string) {}",
			},
			MatchType: MatchModified,
			Score:     1.0,
			Category:  "function",
			Diff: &DiffSummary{
				TotalChanges: 3,
				Inserts:      2,
				Updates:      1,
				Changes: []string{
					"added parameter_declaration in parameter_list",
					"changed identifier: \"int\" -> \"string\"",
				},
			},
		},
	}

	ctx := BuildCompareContextV2(matches, RepoMetrics{}, RepoMetrics{}, "test", nil, nil)

	if !strings.Contains(ctx, "Structural changes") {
		t.Error("context should contain 'Structural changes' section")
	}
	if !strings.Contains(ctx, "added parameter_declaration") {
		t.Error("context should contain diff change description")
	}
}

func TestBuildCompareContextBudget(t *testing.T) {
	// Each symbol body is ~20K chars — well above maxSnippetChars (3000).
	largeBody := strings.Repeat("x", 20_000)

	sym := func(name, file string) *parser.Symbol {
		return makeSymbol(name, "function", file, largeBody)
	}

	// Build enough matches to stress the budget.
	matches := make([]SymbolMatch, 100)
	for i := range 100 {
		matches[i] = SymbolMatch{
			SymbolA:   sym("FuncA", "/repo-a/file.go"),
			SymbolB:   sym("FuncB", "/repo-b/file.go"),
			MatchType: MatchFuzzy,
			Category:  "general",
			Score:     0.5,
		}
	}

	result := BuildCompareContext(matches, RepoMetrics{}, RepoMetrics{}, "budget test")

	t.Run("total output under 200K chars", func(t *testing.T) {
		const limit = 200_000
		if len(result) >= limit {
			t.Errorf("result length %d exceeds budget limit %d", len(result), limit)
		}
	})

	t.Run("snippets are truncated", func(t *testing.T) {
		if !strings.Contains(result, "... (truncated)") {
			t.Error("expected truncated snippet marker in output")
		}
	})
}
