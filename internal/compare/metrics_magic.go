package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/clean"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// countMagicNumbers counts hardcoded numeric literals in a function body.
// Allowed values (0, 1, -1, 2) and small array indices [0], [1], [2] are
// excluded. Const declarations (single-line and multi-line blocks) are
// skipped entirely.
func countMagicNumbers(body, language string) int {
	if body == "" {
		return 0
	}

	stripped := clean.StripComments(body, language)
	count := 0
	inConstBlock := false

	lines := strings.Split(stripped, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track multi-line const blocks: const ( ... )
		if inConstBlock {
			if trimmed == ")" {
				inConstBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "const (") || trimmed == "const(" {
			inConstBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "const ") || trimmed == "const" {
			continue
		}

		count += countMagicInLine(stripStringLiterals(trimmed))
	}

	return count
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
			i++ // skip '-'
			token := extractNumber(runes, &i)
			full := "-" + token
			if isMagic(full) {
				count++
			}
			continue
		}

		if isDigit(runes[i]) {
			// Ensure not part of an identifier (e.g., var1, x2).
			if i > 0 && isIdentChar(runes[i-1]) {
				i++
				continue
			}

			token := extractNumber(runes, &i)
			if isMagic(token) {
				count++
			}
			continue
		}

		i++
	}

	return count
}

// isMagic returns true if the numeric token is a magic number.
// Allowed set: 0, 1, -1, 2 — these are never considered magic.
func isMagic(token string) bool {
	switch token {
	case "0", "1", "2", "-1":
		return false
	}
	return true
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
