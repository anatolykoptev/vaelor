package compare

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestBuildCrossLangReport(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA:   &parser.Symbol{Name: "HandleAuth", File: "handler.go"},
			SymbolB:   &parser.Symbol{Name: "handle_auth", File: "handler.py"},
			MatchType: MatchSemantic,
			Score:     0.87,
		},
		{
			SymbolA:   &parser.Symbol{Name: "FetchData", File: "data.go"},
			SymbolB:   &parser.Symbol{Name: "fetch_data", File: "data.py"},
			MatchType: MatchSemantic,
			Score:     0.79,
		},
		{
			SymbolA:   &parser.Symbol{Name: "Close", File: "conn.go"},
			SymbolB:   &parser.Symbol{Name: "Close", File: "conn.py"},
			MatchType: MatchExact, // not semantic
			Score:     1.0,
		},
	}
	report := BuildCrossLangReport(matches, "go", "python")
	if report == nil {
		t.Fatal("expected report")
	}
	if report.SemanticMatches != 2 {
		t.Errorf("expected 2 semantic matches, got %d", report.SemanticMatches)
	}
	if len(report.TopMatches) != 2 {
		t.Errorf("expected 2 top matches, got %d", len(report.TopMatches))
	}
	if report.LanguageA != "go" || report.LanguageB != "python" {
		t.Error("wrong languages")
	}
}

func TestBuildCrossLangReport_SameLanguage(t *testing.T) {
	report := BuildCrossLangReport(nil, "go", "go")
	if report != nil {
		t.Error("expected nil for same language")
	}
}

func TestBuildCrossLangReport_NoSemanticMatches(t *testing.T) {
	matches := []SymbolMatch{
		{MatchType: MatchExact, Score: 1.0},
	}
	report := BuildCrossLangReport(matches, "go", "python")
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.SemanticMatches != 0 {
		t.Errorf("expected 0, got %d", report.SemanticMatches)
	}
}

func TestFilterEmbeddable_IncludesClass(t *testing.T) {
	syms := []*parser.Symbol{
		{Name: "MyClass", Kind: "class", Body: "class MyClass:"},
		{Name: "foo", Kind: "function", Body: "def foo():"},
		{Name: "Bar", Kind: "type", Body: "type Bar struct{}"},
	}
	result := filterEmbeddable(syms, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 (class + function), got %d", len(result))
	}
}
