// Package preproc tests — helpers + Builder tests.
package preproc

import (
	"strings"
	"testing"
)

// ---- helpers ----------------------------------------------------------------

func assertLineMap(t *testing.T, name string, vs *VirtualSource) {
	t.Helper()
	nl := strings.Count(string(vs.Code), "\n")
	wantLen := nl + 1
	if len(vs.Code) == 0 {
		wantLen = 0
	}
	if len(vs.LineMap) != wantLen {
		t.Errorf("%s: LineMap length = %d, want %d (newlines=%d)", name, len(vs.LineMap), wantLen, nl)
	}
}

func assertLineMapAt(t *testing.T, name string, vs *VirtualSource, virtualLine int, wantOrig uint32) {
	t.Helper()
	idx := virtualLine - 1
	if idx < 0 || idx >= len(vs.LineMap) {
		t.Errorf("%s: virtual line %d out of LineMap range [1..%d]", name, virtualLine, len(vs.LineMap))
		return
	}
	if vs.LineMap[idx] != wantOrig {
		t.Errorf("%s: LineMap[%d] (virtual line %d) = %d, want %d",
			name, idx, virtualLine, vs.LineMap[idx], wantOrig)
	}
}

// ---- Builder direct tests ---------------------------------------------------

func TestBuilder_Empty(t *testing.T) {
	t.Parallel()
	b := NewBuilder("svelte")
	vs := b.Build()
	if len(vs.Code) != 0 {
		t.Errorf("empty builder: Code not empty")
	}
	if len(vs.LineMap) != 0 {
		t.Errorf("empty builder: LineMap not empty")
	}
}

func TestBuilder_SingleBlock(t *testing.T) {
	t.Parallel()
	src := []byte("line1\nline2\nline3\n")
	b := NewBuilder("svelte")
	b.AppendBlock(src, 0, len(src))
	vs := b.Build()

	wantCode := "line1\nline2\nline3\n"
	if string(vs.Code) != wantCode {
		t.Errorf("Code = %q, want %q", string(vs.Code), wantCode)
	}
	// 3 newlines → 4 virtual lines.
	if len(vs.LineMap) != 4 {
		t.Errorf("LineMap len = %d, want 4", len(vs.LineMap))
	}
	// First virtual line maps to original line 1.
	if vs.LineMap[0] != 1 {
		t.Errorf("LineMap[0] = %d, want 1", vs.LineMap[0])
	}
	if vs.LineMap[1] != 2 {
		t.Errorf("LineMap[1] = %d, want 2", vs.LineMap[1])
	}
}

func TestBuilder_BlockAtOffset(t *testing.T) {
	t.Parallel()
	// Block starts at byte 4 ("line3...") — that's after "a\nb\n" (3 lines → line 3 starts).
	src := []byte("a\nb\nline3\nline4\n")
	b := NewBuilder("svelte")
	b.AppendBlock(src, 4, len(src))
	vs := b.Build()

	// Virtual line 1 should map to original line 3.
	if vs.LineMap[0] != 3 {
		t.Errorf("LineMap[0] = %d, want 3", vs.LineMap[0])
	}
	if vs.LineMap[1] != 4 {
		t.Errorf("LineMap[1] = %d, want 4", vs.LineMap[1])
	}
}

func TestBuilder_BlankLinePadding(t *testing.T) {
	t.Parallel()
	src := []byte("aa\nbb\n")
	b := NewBuilder("test")
	b.AppendBlock(src, 0, 3) // "aa\n"
	b.AppendBlankLine()
	b.AppendBlock(src, 3, 6) // "bb\n"
	vs := b.Build()

	if len(vs.LineMap) == 0 {
		t.Fatal("LineMap empty")
	}
	found := false
	for _, v := range vs.LineMap {
		if v == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("no padding (0) entry in LineMap: %v", vs.LineMap)
	}
}
