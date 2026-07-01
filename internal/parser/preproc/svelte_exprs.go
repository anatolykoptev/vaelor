package preproc

// Svelte sigil-aware template-expression scanner.
//
// Unlike Astro (where every top-level {…} is a plain expression — see
// scanMarkupExprRanges), Svelte overloads the brace with block-control sigils:
//
//	{#if EXPR} {#each EXPR as x} {#await EXPR then v} {#key EXPR}   (block open)
//	{:else if EXPR} {:then v} {:catch e} {:else}                   (continuation)
//	{/if} {/each} {/await} {/key}                                  (block close)
//	{@const NAME = EXPR} {@html EXPR} {@render EXPR} {@debug …}     (special)
//	{EXPR}                                                         (plain mustache)
//
// Reaching *effective* control-flow parity with Astro/React means extracting the
// EXPR carried by each header and reparsing it exactly like a plain mustache:
// go-code models NO control-flow-structure edges, so "control-flow parity" is the
// refs/calls INSIDE the construct captured as ordinary call/ref edges — the sigil
// keyword and any binding clause (`as x`, `then v`, the const name) are not part
// of that expression and are excluded. The scan is unambiguous because Svelte
// blocks are prefixed with {#, {:, {/ or {@; a bare { is always a plain mustache.
//
// Bounded failure class (documented, mirrors matchBrace / scanMarkupExprRanges /
// StripGoTemplate in this package):
//   - HTML comments are NOT skipped (only <script>/<style>, via collectSkipRanges)
//     — identical to scanMarkupExprRanges, so Svelte and Astro stay in lockstep.
//   - A keyed-each key clause ({#each items as x (x.id)}) and the {:then}/{:catch}
//     bindings are treated as bindings, not expressions; only the pre-`as` /
//     pre-`then` / pre-`catch` EXPR is surfaced. #snippet / #debug definitions
//     surface nothing.
//   - The as/then/catch boundary and the @const `=` are found by a depth- and
//     string-aware byte scan (scanToKeyword / scanToAssign), so the same letters
//     inside an identifier (`casts`), a string, or a nested call do not fool it;
//     an unbalanced brace or unterminated string inside the header stops the scan.

// ExtractSvelteMarkupExprs batches every Svelte template EXPR — plain mustaches
// and sigil-aware block-header expressions — into ONE reparsable virtual source
// (buildExprVirtualSource), reusing the same batching + line-map core as the
// Astro path. The result carries lang="svelte" and is reparsed with the TSX
// grammar by markupExprReparse.
func ExtractSvelteMarkupExprs(src []byte) *VirtualSource {
	return buildExprVirtualSource(src, scanSvelteExprRanges(src), "svelte")
}

// scanSvelteExprRanges returns the precise EXPR byte ranges of every top-level
// template brace in a Svelte component: the brace-stripped inner content of a
// plain mustache, or the header EXPR sub-range of a block / continuation / special
// tag. <script> and <style> contents are skipped (collectSkipRanges) and
// matchBrace balances nesting. Svelte has no --- frontmatter, so the walk starts
// at 0.
func scanSvelteExprRanges(src []byte) []exprRange {
	skips := collectSkipRanges(src)

	var ranges []exprRange
	i := 0
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
		if r, ok := svelteHeaderExpr(src, i, close); ok && r.end > r.start {
			ranges = append(ranges, r)
		}
		i = close + 1
	}
	return ranges
}

// svelteHeaderExpr classifies the brace at src[open] (closing at src[close]) by
// its sigil and returns the EXPR sub-range to reparse. ok is false for tags that
// carry no expression ({/each}, {:else}, {:then v}, {#snippet …}).
func svelteHeaderExpr(src []byte, open, close int) (exprRange, bool) {
	inner := open + 1
	if inner >= close {
		return exprRange{}, false // {} — empty
	}
	switch src[inner] {
	case '/':
		return exprRange{}, false // {/if} {/each} — close tag, no expr
	case '#':
		return svelteBlockOpenExpr(src, open, close)
	case ':':
		return svelteContinuationExpr(src, open, close)
	case '@':
		return svelteSpecialExpr(src, open, close)
	default:
		return exprRange{start: inner, end: close}, true // plain mustache {EXPR}
	}
}

// svelteBlockOpenExpr handles {#if} {#each} {#await} {#key}. The EXPR runs from
// after the block keyword to the binding boundary: `as` for #each, `then`/`catch`
// for #await, and the whole body for #if/#key.
func svelteBlockOpenExpr(src []byte, open, close int) (exprRange, bool) {
	kwStart := open + 2 // skip '{#'
	kwEnd := scanIdent(src, kwStart, close)
	exprStart := skipSpace(src, kwEnd, close)

	switch string(src[kwStart:kwEnd]) {
	case "if", "key":
		return trimmedRange(src, exprStart, close)
	case "each":
		end := scanToKeyword(src, exprStart, close, "as")
		if end < 0 {
			end = close
		}
		return trimmedRange(src, exprStart, end)
	case "await":
		end := scanToKeyword(src, exprStart, close, "then")
		if end < 0 {
			end = scanToKeyword(src, exprStart, close, "catch")
		}
		if end < 0 {
			end = close
		}
		return trimmedRange(src, exprStart, end)
	default:
		return exprRange{}, false // #snippet or any unrecognized #-keyword — no reparsable EXPR
	}
}

// svelteContinuationExpr handles {:else if EXPR}. Plain {:else} and the {:then v}
// / {:catch e} bindings carry no expression.
func svelteContinuationExpr(src []byte, open, close int) (exprRange, bool) {
	kwStart := open + 2 // skip '{:'
	kwEnd := scanIdent(src, kwStart, close)
	if string(src[kwStart:kwEnd]) != "else" {
		return exprRange{}, false // :then / :catch — bindings
	}
	p := skipSpace(src, kwEnd, close)
	if !matchKeywordAt(src, p, close, "if") {
		return exprRange{}, false // plain {:else}
	}
	return trimmedRange(src, skipSpace(src, p+2, close), close)
}

// svelteSpecialExpr handles {@html EXPR} {@render EXPR} {@debug EXPR} (EXPR after
// the keyword) and {@const NAME = EXPR} (EXPR after the assignment '='). @render
// (Svelte 5 snippet invocation) and @debug reuse the same @-family keyword-EXPR
// path as @html — an accepted superset of the core spec forms, so those two
// expression carriers surface their calls/refs too.
func svelteSpecialExpr(src []byte, open, close int) (exprRange, bool) {
	kwStart := open + 2 // skip '{@'
	kwEnd := scanIdent(src, kwStart, close)

	switch string(src[kwStart:kwEnd]) {
	case "html", "render", "debug":
		return trimmedRange(src, skipSpace(src, kwEnd, close), close)
	case "const":
		eq := scanToAssign(src, skipSpace(src, kwEnd, close), close)
		if eq < 0 {
			return exprRange{}, false
		}
		return trimmedRange(src, skipSpace(src, eq+1, close), close)
	default:
		return exprRange{}, false
	}
}

// trimmedRange builds an exprRange over [start, end) with trailing ASCII
// whitespace trimmed; ok is false if the resulting range is empty.
func trimmedRange(src []byte, start, end int) (exprRange, bool) {
	for end > start && isSpaceByte(src[end-1]) {
		end--
	}
	if start >= end {
		return exprRange{}, false
	}
	return exprRange{start: start, end: end}, true
}

// scanToKeyword returns the index in src of the first whitespace-delimited word
// kw at bracket-depth 0 within [from, end), skipping string literals. Returns -1
// if kw does not occur at depth 0. Used to find the each `as` and await
// `then`/`catch` binding boundaries without being fooled by the same letters
// inside an identifier (`casts`), a string, or a nested call/index.
func scanToKeyword(src []byte, from, end int, kw string) int {
	depth := 0
	i := from
	for i < end {
		switch c := src[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(src, i+1, c, true)
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && matchKeywordAt(src, i, end, kw) {
				return i
			}
		}
		i++
	}
	return -1
}

// scanToAssign returns the index in src of the {@const} assignment '=' at
// bracket-depth 0 within [from, end), skipping string literals and the comparison
// / arrow operators (==, ===, =>, !=, <=, >=). Returns -1 if none.
func scanToAssign(src []byte, from, end int) int {
	depth := 0
	i := from
	for i < end {
		switch c := src[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(src, i+1, c, true)
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 {
				var next, prev byte
				if i+1 < end {
					next = src[i+1]
				}
				if i > from {
					prev = src[i-1]
				}
				if next != '=' && next != '>' && prev != '!' && prev != '<' && prev != '>' && prev != '=' {
					return i
				}
			}
		}
		i++
	}
	return -1
}

// matchKeywordAt reports whether the whitespace-delimited word kw begins at
// src[i] within [i, end). The byte before i (if any) and the byte after the word
// must both be non-word bytes so kw matches a whole token, not a substring
// (`casts` must not match `as`).
func matchKeywordAt(src []byte, i, end int, kw string) bool {
	if i+len(kw) > end {
		return false
	}
	if i > 0 && isWordByte(src[i-1]) {
		return false
	}
	if string(src[i:i+len(kw)]) != kw {
		return false
	}
	if after := i + len(kw); after < end && isWordByte(src[after]) {
		return false
	}
	return true
}

// scanIdent returns the index of the first non-word byte at or after from within
// [from, end).
func scanIdent(src []byte, from, end int) int {
	i := from
	for i < end && isWordByte(src[i]) {
		i++
	}
	return i
}

// skipSpace returns the index of the first non-space byte at or after from within
// [from, end).
func skipSpace(src []byte, from, end int) int {
	i := from
	for i < end && isSpaceByte(src[i]) {
		i++
	}
	return i
}

// isWordByte reports whether b is a JavaScript identifier byte.
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_' || b == '$'
}

// isSpaceByte reports whether b is ASCII whitespace.
func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
