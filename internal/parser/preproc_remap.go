package parser

import (
	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
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
//
// This is the shared remap primitive: RemapSymbolLines (symbol path),
// remapAndAppendRunes (rune classifier), and markupExprReparse (markup call
// sites) all map virtual→original line numbers through it.
func virtualToOriginal(lineMap []uint32, virtualLine uint32) uint32 {
	if virtualLine == 0 || int(virtualLine) > len(lineMap) {
		return 0
	}
	return lineMap[virtualLine-1]
}

// parseVirtualWithRemap parses vs.Code with base's tree-sitter grammar and
// remaps symbol line numbers from virtual to original-file coordinates. This is
// the shared remap core extracted from parseWithTSAndRemap: the grammar/handler
// is a parameter so the grammar is swappable at the call site (plain TypeScript
// today; a markup-expr reparse binds the TSX grammar) without duplicating the
// nil-guard + Parse + RemapSymbolLines sequence.
//
// base.Parse is called directly (not handler.Parse) to avoid re-entering a
// preprocessor handler's Parse and to side-step init-ordering constraints:
// base.caps are read at call time, so they are wired even when base's handler
// init'd after the preprocessor handler that calls this.
//
// path is the ORIGINAL file path — propagated into Symbol.File.
// lang is the preprocessor-language label ("svelte", "astro") — set on the
// empty-result path and on every retained symbol via RemapSymbolLines.
func parseVirtualWithRemap(base *parserBase, path string, vs *preproc.VirtualSource, lang string, opts ParseOpts) (*ParseResult, error) {
	if vs == nil || len(vs.Code) == 0 {
		return &ParseResult{File: path, Language: lang, Symbols: []*Symbol{}, Imports: []string{}}, nil
	}
	result, err := base.Parse(path, vs.Code, opts)
	if err != nil {
		return nil, err
	}
	RemapSymbolLines(result, vs)
	return result, nil
}

// parseWithTSAndRemap parses vs.Code with the TypeScript grammar and remaps
// symbol line numbers from virtual to original-file coordinates. Shared by
// preprocessor-language handlers (Svelte, Astro, Vue).
//
// It is a thin, behaviour-preserving wrapper over parseVirtualWithRemap bound to
// the plain-TypeScript grammar (tsLang): frontmatter and <script> contents are
// plain TS/JS. The markup {expr} reparse path binds the TSX grammar instead
// (see markupExprReparse) so JSX embedded in template expressions parses.
//
// path is the ORIGINAL file path — propagated into Symbol.File.
// lang is the preprocessor-language label ("svelte", "astro") — set on the
// result and on every retained symbol via RemapSymbolLines.
func parseWithTSAndRemap(path string, vs *preproc.VirtualSource, lang string, opts ParseOpts) (*ParseResult, error) {
	return parseVirtualWithRemap(&tsLang.parserBase, path, vs, lang, opts)
}
