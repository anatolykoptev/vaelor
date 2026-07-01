package preproc

// exprRange is the [start, end) byte range of the INNER content of a top-level
// {expr} in the template body (the braces themselves are excluded).
type exprRange struct{ start, end int }

// scanMarkupExprRanges returns the inner byte ranges of every top-level {expr}
// in the template body (after any leading frontmatter). It skips <script> and
// <style> block contents (via collectSkipRanges, the same skip machinery
// scanTemplateRefs uses) and relies on matchBrace for nesting-aware balancing.
//
// Astro template expressions are plain `{expr}` — there are no `{#if}` / `{:else}`
// block sigils (those are Svelte, handled in a later phase), so every top-level
// brace opens an expression. If a brace is never balanced the scan stops at that
// point (documented, matches the bounded behaviour of the other byte-walkers).
func scanMarkupExprRanges(src []byte) []exprRange {
	start := findTemplateStart(src)
	skips := collectSkipRanges(src)

	var ranges []exprRange
	i := start
	for i < len(src) {
		if src[i] != '{' {
			i++
			continue
		}
		if inSkipRanges(i, skips) {
			i++
			continue
		}
		close := matchBrace(src, i)
		if close < 0 {
			break // unbalanced from here on — stop (documented bound)
		}
		if close > i+1 {
			ranges = append(ranges, exprRange{start: i + 1, end: close})
		}
		i = close + 1
	}
	return ranges
}

// ExtractMarkupExprs batches the inner content of ALL top-level {expr} ranges in
// the template body into ONE virtual source (offset-mapped back to original
// coordinates), so a downstream reparse pays a single tree-sitter invocation per
// file rather than one per expression.
//
// It assembles the VirtualSource DIRECTLY and does NOT reuse Builder.AppendBlock
// — deliberately, do not "refactor" it back. Builder.AppendBlock was built for
// <script>/frontmatter spans, which carry their own trailing newline; a bare
// inline {expr} has none. Feeding AppendBlock + AppendBlankLine the 3-expr
// fixture yields the lineMap [8,0,9,0,10,0], and Builder.Build() then trims it
// to countLines(code) entries — silently DROPPING the trailing entries so the
// final block's original-line mapping is lost (a batched {expr} then remaps to
// the wrong source line). Instead, each expression here becomes its own
// statement terminated by a '\n' that maps to NO original line (padding), so the
// next expression's first virtual line stays aligned with its original
// coordinates. The LineMap contract (one entry per virtual content line;
// unmapped/out-of-range lines are padding) matches the existing preproc
// machinery, so virtualToOriginal remaps call sites identically.
//
// The returned VirtualSource carries lang="astro"; it is meant to be reparsed
// with the TSX grammar (a superset that parses the JSX legally embedded in
// template expressions), not the plain-TypeScript grammar.
func ExtractMarkupExprs(src []byte) *VirtualSource {
	ranges := scanMarkupExprRanges(src)
	if len(ranges) == 0 {
		return &VirtualSource{Code: nil, Lang: "astro", LineMap: []uint32{}}
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
	return &VirtualSource{Code: code, Lang: "astro", LineMap: lineMap}
}
