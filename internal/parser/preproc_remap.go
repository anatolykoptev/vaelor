package parser

import (
	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// RemapSymbolLines rewrites StartLine/EndLine on every Symbol in r from
// virtual coordinates (positions within vs.Code) to original-file coordinates
// (positions within the source the virtual code was extracted from).
//
// Symbols whose StartLine maps to 0 in vs.LineMap are padding and get dropped
// from r.Symbols. Symbols whose EndLine maps to 0 have their EndLine left at
// the last mapped line so the range stays non-empty.
//
// Also overrides r.Language with vs.Lang so callers see "svelte"/"astro"
// instead of the embedded "typescript".
func RemapSymbolLines(r *ParseResult, vs *preproc.VirtualSource) {
	if r == nil || vs == nil {
		return
	}
	r.Language = vs.Lang
	if len(vs.LineMap) == 0 {
		return
	}
	mapped := make([]*Symbol, 0, len(r.Symbols))
	for _, sym := range r.Symbols {
		origStart := virtualToOriginal(vs.LineMap, sym.StartLine)
		if origStart == 0 {
			continue // symbol sits on padding — drop
		}
		origEnd := virtualToOriginal(vs.LineMap, sym.EndLine)
		if origEnd == 0 {
			origEnd = origStart
		}
		sym.StartLine = origStart
		sym.EndLine = origEnd
		sym.Language = vs.Lang
		mapped = append(mapped, sym)
	}
	r.Symbols = mapped
}

// virtualToOriginal returns the original file line number for the given 1-based
// virtual line. Returns 0 if the line is out of range or mapped to padding.
func virtualToOriginal(lineMap []uint32, virtualLine uint32) uint32 {
	if virtualLine == 0 || int(virtualLine) > len(lineMap) {
		return 0
	}
	return lineMap[virtualLine-1]
}
