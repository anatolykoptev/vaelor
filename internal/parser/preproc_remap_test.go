package parser

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

func TestRemapSymbolLines(t *testing.T) {
	// Build a synthetic VirtualSource: 5 virtual lines.
	// Virtual line 1 → original 3
	// Virtual line 2 → original 4
	// Virtual line 3 → original 0 (padding)
	// Virtual line 4 → original 10
	// Virtual line 5 → original 11
	vs := &preproc.VirtualSource{
		Code:    []byte("a\nb\n\nd\ne\n"),
		Lang:    "svelte",
		LineMap: []uint32{3, 4, 0, 10, 11},
	}
	result := &ParseResult{
		File:     "test.svelte",
		Language: "typescript",
		Symbols: []*Symbol{
			{Name: "sym_on_line3_padding", StartLine: 3, EndLine: 3},
			{Name: "sym_virt1_to_orig3", StartLine: 1, EndLine: 2},
			{Name: "sym_virt4_5", StartLine: 4, EndLine: 5},
			{Name: "sym_out_of_range", StartLine: 99, EndLine: 99},
		},
	}

	RemapSymbolLines(result, vs)

	if result.Language != "svelte" {
		t.Errorf("Language = %q, want %q", result.Language, "svelte")
	}
	if got := len(result.Symbols); got != 2 {
		t.Fatalf("Symbols length = %d, want 2 (padding + out_of_range dropped); got %v", got, result.Symbols)
	}
	{
		s := result.Symbols[0]
		if s.Name != "sym_virt1_to_orig3" || s.StartLine != 3 || s.EndLine != 4 || s.Language != "svelte" {
			t.Errorf("first symbol wrong: %+v", s)
		}
	}
	{
		s := result.Symbols[1]
		if s.Name != "sym_virt4_5" || s.StartLine != 10 || s.EndLine != 11 || s.Language != "svelte" {
			t.Errorf("second symbol wrong: %+v", s)
		}
	}
}

func TestRemapSymbolLines_Nil(t *testing.T) {
	RemapSymbolLines(nil, nil) // must not panic
	RemapSymbolLines(&ParseResult{}, nil)
	RemapSymbolLines(nil, &preproc.VirtualSource{Lang: "svelte"})
}
