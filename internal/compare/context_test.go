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
