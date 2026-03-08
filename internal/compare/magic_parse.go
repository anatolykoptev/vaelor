package compare

import (
	"strings"
	"unicode"
)

// stripStringLiterals removes content within string delimiters to avoid
// counting digits inside strings.
func stripStringLiterals(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '"' || ch == '\'' || ch == '`' {
			// Skip until matching close quote.
			quote := ch
			i++
			for i < len(s) {
				if s[i] == '\\' && quote != '`' {
					i += 2 // skip escaped char
					continue
				}
				if s[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}
		buf.WriteByte(ch)
		i++
	}
	return buf.String()
}

// extractNumber reads a numeric token starting at runes[*pos] (which is a digit).
// Advances *pos past the number. Handles hex (0x), octal (0o), binary (0b), float.
func extractNumber(runes []rune, pos *int) string {
	var buf strings.Builder
	i := *pos
	n := len(runes)

	// Check for 0x, 0o, 0b prefixes.
	if i < n && runes[i] == '0' && i+1 < n {
		next := unicode.ToLower(runes[i+1])
		if next == 'x' || next == 'o' || next == 'b' {
			buf.WriteRune(runes[i])
			buf.WriteRune(runes[i+1])
			i += 2
			for i < n && isHexDigitOrUnderscore(runes[i]) {
				buf.WriteRune(runes[i])
				i++
			}
			*pos = i
			return buf.String()
		}
	}

	// Regular integer or float.
	for i < n && isDigit(runes[i]) {
		buf.WriteRune(runes[i])
		i++
	}
	// Check for decimal point (float).
	if i < n && runes[i] == '.' && i+1 < n && isDigit(runes[i+1]) {
		buf.WriteRune('.')
		i++
		for i < n && isDigit(runes[i]) {
			buf.WriteRune(runes[i])
			i++
		}
	}
	// Skip exponent (e.g., 1e10).
	if i < n && (runes[i] == 'e' || runes[i] == 'E') {
		buf.WriteRune(runes[i])
		i++
		if i < n && (runes[i] == '+' || runes[i] == '-') {
			buf.WriteRune(runes[i])
			i++
		}
		for i < n && isDigit(runes[i]) {
			buf.WriteRune(runes[i])
			i++
		}
	}

	*pos = i
	return buf.String()
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isHexDigitOrUnderscore(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '_'
}
