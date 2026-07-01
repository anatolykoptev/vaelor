package preproc

// scanMarkupExprRanges returns the inner byte ranges of every top-level {expr}
// in the template body (after any leading frontmatter). It skips <script> and
// <style> block contents (via collectSkipRanges, the same skip machinery
// scanTemplateRefs uses) and relies on matchBrace for nesting-aware balancing.
//
// Astro template expressions are plain `{expr}` — there are no `{#if}` / `{:else}`
// block sigils (those are Svelte — see scanSvelteExprRanges), so every top-level
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
// the Astro template body into ONE virtual source (see buildExprVirtualSource),
// offset-mapped back to original coordinates, so a downstream reparse pays a
// single tree-sitter invocation per file rather than one per expression.
//
// The returned VirtualSource carries lang="astro"; it is meant to be reparsed
// with the TSX grammar (a superset that parses the JSX legally embedded in
// template expressions), not the plain-TypeScript grammar.
func ExtractMarkupExprs(src []byte) *VirtualSource {
	return buildExprVirtualSource(src, scanMarkupExprRanges(src), "astro")
}
