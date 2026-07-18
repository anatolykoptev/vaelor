package parser

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

func TestRemapSymbolLines(t *testing.T) {
	t.Parallel()
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

// TestRemapSymbolLines_EndLinePadding covers the branch where sym.StartLine
// maps to a real original line but sym.EndLine maps to 0 (padding).
// The function must fall back to EndLine = StartLine (origStart) so the
// range stays non-empty.
func TestRemapSymbolLines_EndLinePadding(t *testing.T) {
	t.Parallel()
	// Virtual line map:
	//   virt 1 → orig 5  (real)
	//   virt 2 → orig 0  (padding)
	//   virt 3 → orig 7  (real)
	vs := &preproc.VirtualSource{
		Code:    []byte("a\nb\nc\n"),
		Lang:    "astro",
		LineMap: []uint32{5, 0, 7},
	}
	result := &ParseResult{
		File:     "test.astro",
		Language: "typescript",
		Symbols: []*Symbol{
			// StartLine=1 (→ orig 5, real), EndLine=2 (→ orig 0, padding).
			// Expected: symbol kept, EndLine falls back to StartLine (5).
			{Name: "end_in_padding", StartLine: 1, EndLine: 2},
		},
	}

	RemapSymbolLines(result, vs)

	if got := len(result.Symbols); got != 1 {
		t.Fatalf("Symbols length = %d, want 1", got)
	}
	s := result.Symbols[0]
	if s.StartLine != 5 {
		t.Errorf("StartLine = %d, want 5", s.StartLine)
	}
	if s.EndLine != 5 {
		t.Errorf("EndLine = %d, want 5 (fallback to StartLine); EndLine→padding must not propagate zero", s.EndLine)
	}
	if s.Language != "astro" {
		t.Errorf("Language = %q, want %q", s.Language, "astro")
	}
}

func TestRemapSymbolLines_Nil(t *testing.T) {
	t.Parallel()
	RemapSymbolLines(nil, nil) // must not panic
	RemapSymbolLines(&ParseResult{}, nil)
	RemapSymbolLines(nil, &preproc.VirtualSource{Lang: "svelte"})
}
