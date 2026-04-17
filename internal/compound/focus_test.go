package compound_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestFilterByFocus(t *testing.T) {
	sym1 := &parser.Symbol{Name: "toggle", Kind: parser.KindFunction, File: "/host/src/piter-now/frontend/src/components/ThemeToggle.svelte", StartLine: 8}
	sym2 := &parser.Symbol{Name: "toggle", Kind: parser.KindFunction, File: "/host/src/piter-now/frontend/src/components/Filters.svelte", StartLine: 3}
	sym3 := &parser.Symbol{Name: "toggle", Kind: parser.KindFunction, File: "/host/src/other/lib/util.ts", StartLine: 1}

	all := []*parser.Symbol{sym1, sym2, sym3}

	tests := []struct {
		name      string
		focus     string
		wantFiles []string
	}{
		{
			name:      "empty focus returns all",
			focus:     "",
			wantFiles: []string{sym1.File, sym2.File, sym3.File},
		},
		{
			name:      "bare filename suffix match",
			focus:     "ThemeToggle.svelte",
			wantFiles: []string{sym1.File},
		},
		{
			name:      "partial path substring match",
			focus:     "components/Filters",
			wantFiles: []string{sym2.File},
		},
		{
			name:      "exact file path match",
			focus:     "/host/src/other/lib/util.ts",
			wantFiles: []string{sym3.File},
		},
		{
			name:      "nonsense focus returns no match",
			focus:     "does_not_exist.go",
			wantFiles: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compound.FilterByFocus(all, tc.focus)
			if len(got) != len(tc.wantFiles) {
				t.Fatalf("focus=%q: want %d results, got %d", tc.focus, len(tc.wantFiles), len(got))
			}
			for i, sym := range got {
				if sym.File != tc.wantFiles[i] {
					t.Errorf("focus=%q result[%d]: want file %q, got %q", tc.focus, i, tc.wantFiles[i], sym.File)
				}
			}
		})
	}
}
