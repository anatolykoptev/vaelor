package semhealth

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

func TestComputeSemanticDupRatio(t *testing.T) {
	tests := []struct {
		name       string
		pairs      []embeddings.SimilarPair
		totalFuncs int
		wantMin    float64
		wantMax    float64
	}{
		{
			name:       "no pairs",
			pairs:      nil,
			totalFuncs: 10,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name: "one pair out of 10",
			pairs: []embeddings.SimilarPair{
				{SymbolA: "Foo", FileA: "a.go", SymbolB: "Bar", FileB: "b.go", Similarity: 0.95},
			},
			totalFuncs: 10,
			wantMin:    0.19,
			wantMax:    0.21, // 2/10 = 0.20
		},
		{
			name:       "zero funcs",
			pairs:      nil,
			totalFuncs: 0,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name: "overlapping pairs share symbols",
			pairs: []embeddings.SimilarPair{
				{SymbolA: "A", FileA: "x.go", SymbolB: "B", FileB: "y.go", Similarity: 0.95},
				{SymbolA: "A", FileA: "x.go", SymbolB: "C", FileB: "z.go", Similarity: 0.93},
			},
			totalFuncs: 10,
			wantMin:    0.29,
			wantMax:    0.31, // 3 unique symbols / 10 = 0.30
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := ComputeSemanticDupRatio(tt.pairs, tt.totalFuncs)
			if ratio < tt.wantMin || ratio > tt.wantMax {
				t.Errorf("ratio = %f, want [%f, %f]", ratio, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCollectDupGroups(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{SymbolA: "Foo", FileA: "a.go", LineA: 1, SymbolB: "Bar", FileB: "b.go", LineB: 5, Similarity: 0.96},
		{SymbolA: "Foo", FileA: "a.go", LineA: 1, SymbolB: "Baz", FileB: "c.go", LineB: 10, Similarity: 0.94},
		{SymbolA: "X", FileA: "d.go", LineA: 20, SymbolB: "Y", FileB: "e.go", LineB: 30, Similarity: 0.93},
	}

	groups := CollectDupGroups(pairs)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	// First group should have 3 symbols (Foo, Bar, Baz) — largest first.
	if len(groups[0].Symbols) != 3 {
		t.Errorf("group[0] has %d symbols, want 3", len(groups[0].Symbols))
	}
	// Second group has 2 symbols (X, Y).
	if len(groups[1].Symbols) != 2 {
		t.Errorf("group[1] has %d symbols, want 2", len(groups[1].Symbols))
	}

	// AvgSimilarity should be set.
	if groups[0].AvgSimilarity < 0.90 {
		t.Errorf("group[0].AvgSimilarity = %f, want >= 0.90", groups[0].AvgSimilarity)
	}
	if groups[1].AvgSimilarity < 0.90 {
		t.Errorf("group[1].AvgSimilarity = %f, want >= 0.90", groups[1].AvgSimilarity)
	}
}

func TestCollectDupGroupsEmpty(t *testing.T) {
	groups := CollectDupGroups(nil)
	if groups != nil {
		t.Errorf("expected nil for empty pairs, got %v", groups)
	}
}

func TestSemanticResult(t *testing.T) {
	r := &SemanticResult{
		SemanticDupRatio: 0.15,
		DupGroups: []DupGroup{
			{Symbols: []DupSymbol{{Name: "A", File: "a.go"}, {Name: "B", File: "b.go"}}},
		},
	}
	if r.SemanticDupRatio != 0.15 {
		t.Errorf("ratio = %f, want 0.15", r.SemanticDupRatio)
	}
	if len(r.DupGroups) != 1 {
		t.Errorf("groups = %d, want 1", len(r.DupGroups))
	}
}

func TestFormatDupGroupMessage(t *testing.T) {
	g := DupGroup{
		Symbols: []DupSymbol{
			{Name: "Foo", File: "a.go"},
			{Name: "Bar", File: "b.go"},
		},
	}
	msg := FormatDupGroupMessage(g)
	if msg != "Foo (a.go), Bar (b.go)" {
		t.Errorf("FormatDupGroupMessage = %q", msg)
	}
}

func TestAnalyzeNilStore(t *testing.T) {
	result := Analyze(context.TODO(), nil, "test", 10)
	if result != nil {
		t.Error("expected nil for nil store")
	}
}

func TestAnalyzeEmptyRepoKey(t *testing.T) {
	result := Analyze(context.TODO(), nil, "", 10)
	if result != nil {
		t.Error("expected nil for empty repoKey")
	}
}

func TestAnalyzeZeroFuncs(t *testing.T) {
	result := Analyze(context.TODO(), nil, "test", 0)
	if result != nil {
		t.Error("expected nil for zero totalFuncs")
	}
}
