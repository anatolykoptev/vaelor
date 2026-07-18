package compare

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/clean"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// countMagicNumbers counts hardcoded numeric literals in a function body.
// Allowed values {0, 1, 2, -1} and their float equivalents (0.0, 1.0, 2.0, -1.0)
// are excluded via SonarQube-style float normalization. SQL-style positional
// parameters ($1, $2), single-digit array indices ([3]..[9]), nolint:mnd
// directives, and const declarations are skipped entirely.
func countMagicNumbers(body, language string) int {
	if body == "" {
		return 0
	}

	// Pre-scan original body for nolint:mnd directives (before comment stripping).
	nolintSet := findNolintLines(body)

	stripped := clean.StripComments(body, language)
	count := 0
	inConstBlock := false

	lines := strings.Split(stripped, "\n")
	for lineIdx, line := range lines {
		if nolintSet[lineIdx] {
			continue
		}

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

// findNolintLines returns a set of line indices that contain //nolint:mnd.
func findNolintLines(body string) map[int]bool {
	result := make(map[int]bool)
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "nolint:mnd") {
			result[i] = true
		}
	}
	return result
}

// countMagicInLine scans a single line for numeric literals and counts magic ones.
func countMagicInLine(line string) int {
	count := 0
	runes := []rune(line)
	n := len(runes)

	for i := 0; i < n; {
		if runes[i] == '-' && i+1 < n && isDigit(runes[i+1]) {
			if c, adv := checkNegativeNumber(runes, i, n); adv > 0 {
				count += c
				i += adv
				continue
			}
		}

		if isDigit(runes[i]) {
			if shouldSkipDigit(runes, i, n) {
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

// checkNegativeNumber handles '-' followed by digit. Returns (magic count, advance).
// Returns (0, 0) if '-' is preceded by an identifier char (e.g., x-1).
func checkNegativeNumber(runes []rune, i, n int) (int, int) {
	if i > 0 && isIdentChar(runes[i-1]) {
		return 0, 0
	}
	pos := i + 1 // skip '-'
	token := extractNumber(runes, &pos)
	count := 0
	if isMagic("-" + token) {
		count = 1
	}
	return count, pos - i
}

// shouldSkipDigit returns true if the digit at runes[i] should not be treated
// as a standalone numeric literal: identifiers (var1), SQL params ($1), or
// single-digit array indices ([3]).
func shouldSkipDigit(runes []rune, i, n int) bool {
	if i == 0 {
		return false
	}
	prev := runes[i-1]
	return isIdentChar(prev) || prev == '$' || (prev == '[' && isSingleDigitIndex(runes, i, n))
}

// isMagic returns true if the numeric token is a magic number.
// Uses SonarQube-style float normalization: 1.0 → 1, 0.0 → 0.
// Allowed set after normalization: {-1, 0, 1, 2}.
func isMagic(token string) bool {
	normalized := normalizeFloat(token)
	switch normalized {
	case "0", "1", "2", "-1":
		return false
	}
	return true
}

// MagicNumberEntry describes a single function containing magic numbers.
type MagicNumberEntry struct {
	Name     string
	File     string
	Line     int
	Count    int
	Language string
}

// CollectMagicNumbers returns all functions/methods that contain magic numbers.
// Test files are excluded. Results are sorted by count descending.
func CollectMagicNumbers(snap *RepoSnapshot) []MagicNumberEntry {
	prefix := snap.Root + "/"
	var entries []MagicNumberEntry

	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if isTestFile(sym.File) {
			continue
		}
		n := countMagicNumbers(sym.Body, sym.Language)
		if n == 0 {
			continue
		}
		entries = append(entries, MagicNumberEntry{
			Name:     sym.Name,
			File:     strings.TrimPrefix(sym.File, prefix),
			Line:     int(sym.StartLine),
			Count:    n,
			Language: sym.Language,
		})
	}

	// Sort by count descending.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Count > entries[j-1].Count; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	return entries
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
