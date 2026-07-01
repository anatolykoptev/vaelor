package preproc

// exprRange is a [start, end) byte range, in ORIGINAL source coordinates, of an
// expression to be reparsed. For a plain markup mustache ({expr}) it is the
// brace-stripped inner content; for a Svelte block header ({#each EXPR as x}) it
// is the precise EXPR sub-range (the sigil keyword and any binding clause are
// excluded). Both the Astro range scanner (scanMarkupExprRanges) and the
// sigil-aware Svelte scanner (scanSvelteExprRanges) emit this shape, and
// buildExprVirtualSource batches either into one reparsable virtual source.
type exprRange struct{ start, end int }

// buildExprVirtualSource batches the bytes of every range into ONE virtual
// source, offset-mapped back to original coordinates, so a downstream reparse
// pays a single tree-sitter invocation per file rather than one per expression.
// It is the shared batching + line-map core behind both ExtractMarkupExprs (Astro
// plain {expr}) and ExtractSvelteMarkupExprs (Svelte {expr} + sigil-aware
// block-header EXPR) — the two differ only in their range scanner.
//
// It assembles the VirtualSource DIRECTLY and does NOT reuse Builder.AppendBlock
// — deliberately, do not "refactor" it back. Builder.AppendBlock was built for
// <script>/frontmatter spans, which carry their own trailing newline; a bare
// inline expression has none. Feeding AppendBlock + AppendBlankLine the 3-expr
// fixture yields the lineMap [8,0,9,0,10,0], and Builder.Build() then trims it to
// countLines(code) entries — silently DROPPING the trailing entries so the final
// block's original-line mapping is lost (a batched expr then remaps to the wrong
// source line). Instead, each expression here becomes its own statement
// terminated by a '\n' that maps to NO original line (padding), so the next
// expression's first virtual line stays aligned with its original coordinates.
// The LineMap contract (one entry per virtual content line; unmapped/out-of-range
// lines are padding) matches the existing preproc machinery, so virtualToOriginal
// remaps call sites identically.
//
// The returned VirtualSource carries the given lang label; it is meant to be
// reparsed with the TSX grammar (a superset that parses the JSX legally embedded
// in template expressions), not the plain-TypeScript grammar.
func buildExprVirtualSource(src []byte, ranges []exprRange, lang string) *VirtualSource {
	if len(ranges) == 0 {
		return &VirtualSource{Code: nil, Lang: lang, LineMap: []uint32{}}
	}
	var code []byte
	var lineMap []uint32
	for _, r := range ranges {
		line, _ := lineColAt(src, r.start)
		origLine := uint32(line)
		lineMap = append(lineMap, origLine) // first virtual line of this block
		for _, c := range src[r.start:r.end] {
			code = append(code, c)
			if c == '\n' {
				origLine++
				lineMap = append(lineMap, origLine)
			}
		}
		code = append(code, '\n') // statement terminator — maps to no original line
	}
	return &VirtualSource{Code: code, Lang: lang, LineMap: lineMap}
}
