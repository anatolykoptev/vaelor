package compare

import (
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/clean"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// countMagicNumbers counts hardcoded numeric literals in a function body.
// Allowed values (0, 1, -1, 2) and small array indices [0], [1], [2] are
// excluded. Const declarations are skipped entirely.
func countMagicNumbers(body, language string) int {
	if body == "" {
		return 0
	}

	stripped := clean.StripComments(body, language)
	count := 0

	lines := strings.Split(stripped, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "const ") || trimmed == "const" {
			continue
		}
		count += countMagicInLine(stripStringLiterals(trimmed))
	}

	return count
}

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

// countMagicInLine scans a single line for numeric literals and counts magic ones.
func countMagicInLine(line string) int {
	count := 0
	runes := []rune(line)
	n := len(runes)

	for i := 0; i < n; {
		// Check for negative number: '-' followed by digit, not part of identifier.
		if runes[i] == '-' && i+1 < n && isDigit(runes[i+1]) {
			// Ensure '-' is not preceded by an identifier char.
			if i > 0 && isIdentChar(runes[i-1]) {
				i++
				continue
			}
			start := i
			i++ // skip '-'
			token := extractNumber(runes, &i)
			full := "-" + token
			if isMagic(full, false) {
				count++
			}
			_ = start
			continue
		}

		if isDigit(runes[i]) {
			// Ensure not part of an identifier (e.g., var1, x2).
			if i > 0 && isIdentChar(runes[i-1]) {
				i++
				continue
			}
			// Check if preceded by '[' (array index context).
			arrayCtx := false
			for j := i - 1; j >= 0; j-- {
				if runes[j] == ' ' || runes[j] == '\t' {
					continue
				}
				if runes[j] == '[' {
					arrayCtx = true
				}
				break
			}

			token := extractNumber(runes, &i)
			if isMagic(token, arrayCtx) {
				count++
			}
			continue
		}

		i++
	}

	return count
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

// isMagic returns true if the numeric token is a magic number.
// Allowed set: 0, 1, -1, 2. In array context, [0], [1], [2] are also allowed.
func isMagic(token string, arrayCtx bool) bool {
	switch token {
	case "0", "1", "2", "-1":
		return false
	}
	if arrayCtx {
		switch token {
		case "0", "1", "2":
			return false
		}
	}
	return true
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

// computeMagicNumberRatio returns the ratio of functions/methods containing
// at least one magic number. Test files are excluded.
func computeMagicNumberRatio(symbols []*parser.Symbol) float64 {
	total := 0
	withMagic := 0

	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if isTestFile(sym.File) {
			continue
		}
		total++
		if countMagicNumbers(sym.Body, sym.Language) > 0 {
			withMagic++
		}
	}

	if total == 0 {
		return 0
	}
	return float64(withMagic) / float64(total)
}
