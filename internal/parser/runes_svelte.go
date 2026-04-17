package parser

import (
	"context"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// collectRuneSymbols parses src with the TypeScript grammar and returns KindRune
// symbols at their raw (1-based) source line numbers — no coordinate remapping.
// Suitable for .svelte.ts/.svelte.js files where the source is the whole file,
// and for svelteHandler which remaps afterwards via appendRuneSymbols.
func collectRuneSymbols(src []byte, path string) []*Symbol {
	if len(src) == 0 {
		return nil
	}
	caps := tsLang.Capabilities()
	if caps.SitterLanguage == nil {
		return nil
	}
	ps := sitter.NewParser()
	defer ps.Close()
	ps.SetLanguage(caps.SitterLanguage)

	tree, err := ps.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil
	}
	defer tree.Close()

	var syms []*Symbol
	walkRuneNodes(tree.RootNode(), src, &syms, path)
	return syms
}

// appendRuneSymbols collects rune symbols from vs.Code (virtual coordinates),
// remaps them to original-file coordinates using vs.LineMap, and appends them
// to result.Symbols.
//
// Called from svelteHandler.Parse after parseWithTSAndRemap.
func appendRuneSymbols(result *ParseResult, vs *preproc.VirtualSource, path string) {
	if vs == nil || len(vs.Code) == 0 {
		return
	}
	syms := collectRuneSymbols(vs.Code, path)

	// Remap virtual line numbers to original .svelte coordinates.
	for _, sym := range syms {
		origStart := virtualToOriginal(vs.LineMap, sym.StartLine)
		if origStart == 0 {
			continue // on padding — drop
		}
		origEnd := virtualToOriginal(vs.LineMap, sym.EndLine)
		if origEnd == 0 {
			origEnd = origStart
		}
		sym.StartLine = origStart
		sym.EndLine = origEnd
		sym.Language = vs.Lang
		result.Symbols = append(result.Symbols, sym)
	}
}
