package preproc

// matchBrace returns the byte index of the '}' that closes the '{' at src[open].
// It tracks brace nesting depth and skips the contents of ', ", and ` string
// literals (via the shared skipQuoted) so braces that appear inside strings are
// not counted. Returns -1 if the opening brace is never balanced.
//
// This is the small shared balanced-brace primitive the NEW markup {expr}
// scanner is built on (scanMarkupExprRanges). It is deliberately NOT a full
// JS/JSX parser — the tsxLang reparse downstream is what actually parses the
// expression; this scanner only needs to find the expression's outer bounds.
//
// Bounded failure class (documented, mirrors the limits StripGoTemplate and
// scanTemplateRefs already document in this package):
//   - Template-literal `${...}` interpolations are skipped as ordinary string
//     content between backticks, so their inner braces do not affect depth.
//     This is correct for the common `${x}` case; a stray *unmatched* brace
//     inside a template string could mis-balance the scan.
//   - `<` / `>` and JSX are treated as ordinary bytes (only braces and string
//     delimiters drive the scan); JSX embedded in an expression never carries
//     unbalanced braces, so the outer bounds stay correct.
func matchBrace(src []byte, open int) int {
	depth := 0
	i := open
	for i < len(src) {
		switch src[i] {
		case '\'', '"', '`':
			// JS strings (all three delimiters, incl. template literals) process
			// backslash escapes, so escaped=true.
			i = skipQuoted(src, i+1, src[i], true)
			continue
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}
