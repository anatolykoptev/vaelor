package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

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
			name: "missing in B",
			match: SymbolMatch{
				SymbolA: &parser.Symbol{Name: "Foo"},
			},
			expect: "B",
		},
		{
			name: "missing in A",
			match: SymbolMatch{
				SymbolB: &parser.Symbol{Name: "Bar"},
			},
			expect: "A",
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
