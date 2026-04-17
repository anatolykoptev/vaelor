package parser

import (
	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// RemapSymbolLines rewrites StartLine/EndLine on every Symbol in r from
// virtual coordinates (positions within vs.Code) to original-file coordinates
// (positions within the source the virtual code was extracted from).
//
// Side effects (all in place):
//   - r.Language is set to vs.Lang.
//   - Each retained Symbol has its Language field overwritten with vs.Lang
//     (callers must not rely on the pre-call Language of individual symbols).
//   - Symbols whose StartLine maps to 0 in vs.LineMap are padding and are
//     dropped from r.Symbols.
//   - Symbols whose EndLine maps to 0 keep their StartLine as EndLine so the
//     range stays non-empty.
//
// Pointer identity of RETAINED symbols is preserved — caller-held *Symbol
// pointers remain valid but their fields have been mutated.
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

// parseWithTSAndRemap parses vs.Code with the TypeScript grammar and remaps
// symbol line numbers from virtual to original-file coordinates. Shared by
// preprocessor-language handlers (Svelte, Astro).
//
// Uses tsLang.parserBase.Parse directly (not handler.Parse) to avoid
// re-entering the preprocessor's Parse and to side-step init-ordering
// constraints (tsLang caps are wired when this is called at parse time,
// even if the preprocessor handler was init'd first).
//
// path is the ORIGINAL file path — propagated into Symbol.File.
// lang is the preprocessor-language label ("svelte", "astro") — set on the
// result and on every retained symbol via RemapSymbolLines.
func parseWithTSAndRemap(path string, vs *preproc.VirtualSource, lang string, opts ParseOpts) (*ParseResult, error) {
	if vs == nil || len(vs.Code) == 0 {
		return &ParseResult{File: path, Language: lang, Symbols: []*Symbol{}, Imports: []string{}}, nil
	}
	result, err := tsLang.parserBase.Parse(path, vs.Code, opts)
	if err != nil {
		return nil, err
	}
	RemapSymbolLines(result, vs)
	return result, nil
}
