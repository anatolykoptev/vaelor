package preproc

// skipQuoted advances past a quoted string and returns the index of the first
// byte AFTER the closing delimiter, or len(src) if the string is unterminated.
// `from` is the index just AFTER the opening delimiter `q`.
//
// When escaped is true a backslash escapes the next byte — the semantics of Go
// interpreted "…" strings and all JavaScript strings ('…', "…", and `…`
// template literals). When escaped is false backslashes are literal — the
// semantics of Go raw `…` strings. Making it a per-caller flag keeps each
// caller correct: the Go-template backtick scanner (raw strings) passes false,
// the JS/TSX brace scanner passes true.
//
// This is the single quote-skipper shared by the Go-template action scanner
// (findActionClose) and the markup-expression brace scanner (matchBrace); it
// replaces the previously duplicated skipDoubleQuoted / skipBacktickQuoted /
// skipString copies.
func skipQuoted(src []byte, from int, q byte, escaped bool) int {
	i := from
	for i < len(src) {
		c := src[i]
		if escaped && c == '\\' {
			i += 2
			continue
		}
		if c == q {
			return i + 1
		}
		i++
	}
	return len(src)
}
