package compare

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestMatchExact verifies that symbols with identical name+kind are paired as
// exact matches and symbols with no counterpart become gaps.
func TestMatchExact(t *testing.T) {
	symA1 := &parser.Symbol{Name: "ServeHTTP", Kind: parser.KindMethod}
	symA2 := &parser.Symbol{Name: "NewServer", Kind: parser.KindFunction}
	symA3 := &parser.Symbol{Name: "OnlyInA", Kind: parser.KindFunction}

	symB1 := &parser.Symbol{Name: "ServeHTTP", Kind: parser.KindMethod}
	symB2 := &parser.Symbol{Name: "NewServer", Kind: parser.KindFunction}
	symB3 := &parser.Symbol{Name: "OnlyInB", Kind: parser.KindType}

	matches := MatchSymbols(context.Background(),
		[]*parser.Symbol{symA1, symA2, symA3},
		[]*parser.Symbol{symB1, symB2, symB3},
		nil,
	)

	var exactCount, gapCount int
	for _, m := range matches {
		switch m.MatchType {
		case MatchExact:
			exactCount++
		case MatchGap:
			gapCount++
		}
	}

	if exactCount != 2 {
		t.Errorf("exact matches: got %d, want 2", exactCount)
	}
	if gapCount != 2 {
		t.Errorf("gap matches: got %d, want 2", gapCount)
	}
	if total := len(matches); total != 4 {
		t.Errorf("total matches: got %d, want 4", total)
	}
}

// TestMatchFuzzy verifies that symbols with similar names and same kind are
// paired as fuzzy matches when they do not match exactly.
// "HandleRequest" vs "HandleRequests" differs by 1 char → similarity ≈ 0.93 (above threshold 0.7).
func TestMatchFuzzy(t *testing.T) {
	symA := &parser.Symbol{Name: "HandleRequest", Kind: parser.KindFunction}
	symB := &parser.Symbol{Name: "HandleRequests", Kind: parser.KindFunction}

	matches := MatchSymbols(context.Background(),
		[]*parser.Symbol{symA},
		[]*parser.Symbol{symB},
		nil,
	)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]
	if m.MatchType != MatchFuzzy {
		t.Errorf("match type: got %q, want %q", m.MatchType, MatchFuzzy)
	}
	if m.SymbolA != symA {
		t.Error("SymbolA mismatch")
	}
	if m.SymbolB != symB {
		t.Error("SymbolB mismatch")
	}
	if m.Score < fuzzyThreshold {
		t.Errorf("score %f below threshold %f", m.Score, fuzzyThreshold)
	}
}

// TestMatchFuzzyDifferentKind verifies that symbols with similar names but
// different kinds are NOT fuzzy-matched.
func TestMatchFuzzyDifferentKind(t *testing.T) {
	symA := &parser.Symbol{Name: "Handler", Kind: parser.KindFunction}
	symB := &parser.Symbol{Name: "Handler", Kind: parser.KindType}

	matches := MatchSymbols(context.Background(),
		[]*parser.Symbol{symA},
		[]*parser.Symbol{symB},
		nil,
	)

	for _, m := range matches {
		if m.MatchType == MatchExact || m.MatchType == MatchFuzzy {
			t.Errorf("expected gap only, got %q", m.MatchType)
		}
	}
}

// TestLevenshteinDistance verifies edit-distance calculations for known pairs.
func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}

	for _, tc := range tests {
		t.Run(tc.a+"/"+tc.b, func(t *testing.T) {
			got := levenshtein(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestMatchSemantic verifies that the classifier is called for remaining
// unmatched symbols and its results are included.
func TestMatchSemantic(t *testing.T) {
	symA := &parser.Symbol{Name: "ProcessData", Kind: parser.KindFunction}
	symB := &parser.Symbol{Name: "HandleData", Kind: parser.KindFunction}

	fake := &fakeClassifier{
		result: []SymbolMatch{
			{SymbolA: symA, SymbolB: symB, MatchType: MatchSemantic, Score: 0.9},
		},
	}

	matches := MatchSymbols(context.Background(),
		[]*parser.Symbol{symA},
		[]*parser.Symbol{symB},
		fake,
	)

	if !fake.called {
		t.Error("classifier was not called")
	}

	var semanticCount int
	for _, m := range matches {
		if m.MatchType == MatchSemantic {
			semanticCount++
		}
	}
	if semanticCount != 1 {
		t.Errorf("semantic matches: got %d, want 1", semanticCount)
	}
}

// TestMatchExact_DistinguishIdenticalFromModified verifies that exact-name
// matches are split into MatchExact (identical body) and MatchModified
// (same name+kind but different body hash).
func TestMatchExact_DistinguishIdenticalFromModified(t *testing.T) {
	symbolsA := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, Body: "func Foo() { return 1 }", BodyHash: 111},
		{Name: "Bar", Kind: parser.KindFunction, Body: "func Bar() { return 2 }", BodyHash: 222},
	}
	symbolsB := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, Body: "func Foo() { return 1 }", BodyHash: 111},
		{Name: "Bar", Kind: parser.KindFunction, Body: "func Bar() { return 99 }", BodyHash: 333},
	}

	matches := MatchSymbols(context.Background(), symbolsA, symbolsB, nil)

	var identicalCount, modifiedCount int
	for _, m := range matches {
		switch m.MatchType {
		case MatchExact:
			identicalCount++
		case MatchModified:
			modifiedCount++
		}
	}

	if identicalCount != 1 {
		t.Errorf("identicalCount = %d, want 1", identicalCount)
	}
	if modifiedCount != 1 {
		t.Errorf("modifiedCount = %d, want 1", modifiedCount)
	}
}

// TestMatchSignature_CatchRename verifies that symbols with different names
// but identical signature and body hash are matched as renamed.
func TestMatchSignature_CatchRename(t *testing.T) {
	symbolsA := []*parser.Symbol{
		{Name: "HandleRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context, req *Request) error", BodyHash: 555},
	}
	symbolsB := []*parser.Symbol{
		{Name: "ProcessRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context, req *Request) error", BodyHash: 555},
	}

	matches := MatchSymbols(context.Background(), symbolsA, symbolsB, nil)

	found := false
	for _, m := range matches {
		if m.SymbolA != nil && m.SymbolB != nil {
			found = true
			if m.MatchType != MatchRenamed {
				t.Errorf("MatchType = %q, want %q", m.MatchType, MatchRenamed)
			}
			if m.Score < 0.8 {
				t.Errorf("Score = %.2f, want >= 0.8", m.Score)
			}
		}
	}
	if !found {
		t.Error("expected renamed match, got none")
	}
}

// TestMatchSignature_DifferentSignature_NoMatch verifies that symbols with
// different names AND different signatures are not matched as renamed.
func TestMatchSignature_DifferentSignature_NoMatch(t *testing.T) {
	symbolsA := []*parser.Symbol{
		{Name: "HandleRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context) error"},
	}
	symbolsB := []*parser.Symbol{
		{Name: "ProcessData", Kind: parser.KindFunction, Signature: "func(data []byte) (int, error)"},
	}

	matches := MatchSymbols(context.Background(), symbolsA, symbolsB, nil)

	for _, m := range matches {
		if m.SymbolA != nil && m.SymbolB != nil && m.MatchType == MatchRenamed {
			t.Error("should not match different signatures as renamed")
		}
	}
}

// TestMatchSymbols_Moved verifies that a symbol with the same name, kind, and
// body hash but different file path is classified as MatchMoved.
func TestMatchSymbols_Moved(t *testing.T) {
	a := []*parser.Symbol{
		{Name: "HandleAuth", Kind: "function", File: "pkg/auth/handler.go",
			Signature: "func HandleAuth()", BodyHash: 12345},
	}
	b := []*parser.Symbol{
		{Name: "HandleAuth", Kind: "function", File: "internal/auth/handler.go",
			Signature: "func HandleAuth()", BodyHash: 12345},
	}
	matches := MatchSymbols(context.Background(), a, b, nil)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].MatchType != MatchMoved {
		t.Errorf("expected MatchMoved, got %s", matches[0].MatchType)
	}
}

// TestMatchSymbols_NotMovedWhenSameFile verifies that a symbol with the same
// name, kind, body hash, and file path is classified as MatchExact (not MatchMoved).
func TestMatchSymbols_NotMovedWhenSameFile(t *testing.T) {
	a := []*parser.Symbol{
		{Name: "HandleAuth", Kind: "function", File: "pkg/auth/handler.go",
			Signature: "func HandleAuth()", BodyHash: 12345},
	}
	b := []*parser.Symbol{
		{Name: "HandleAuth", Kind: "function", File: "pkg/auth/handler.go",
			Signature: "func HandleAuth()", BodyHash: 12345},
	}
	matches := MatchSymbols(context.Background(), a, b, nil)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].MatchType != MatchExact {
		t.Errorf("expected MatchExact, got %s", matches[0].MatchType)
	}
}

type fakeClassifier struct {
	called bool
	result []SymbolMatch
}

func (f *fakeClassifier) ClassifySymbols(_, _ []*parser.Symbol) ([]SymbolMatch, error) {
	f.called = true
	return f.result, nil
}
