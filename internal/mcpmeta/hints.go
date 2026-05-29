// Package mcpmeta hints.go — calibrated next-call hint helpers.
//
// Calibration philosophy (CLAUDE.md mcpmeta rule):
//
//	"Hint populated only when a clear next-call is cheap and obvious.
//	 A noisy hint trains the calling agent to ignore the field."
//
// All helpers default to "" (silent) and only fire on tight conditions.
package mcpmeta

import (
	"fmt"
	"regexp"
	"strings"
)

// explainQueryRE recognises questions that want prose, not chaining.
var explainQueryRE = regexp.MustCompile(`(?i)\b(why|how|describe|explain)\b`)

// HintAfterCodeSearch returns a calibrated chaining hint for a code_search
// response, or "" when no hint is warranted.
//
// Rules:
//   - explain-class query → silent (caller wants prose, not chaining)
//   - exactly 1 hit → suggest understand(symbol=...) on that symbol
//   - 0 or >1 hits → silent (too many to pin one symbol)
func HintAfterCodeSearch(query string, nHits int, firstHitSymbol string) string {
	if explainQueryRE.MatchString(query) {
		return ""
	}
	if nHits != 1 || firstHitSymbol == "" {
		return ""
	}
	return fmt.Sprintf(
		"single hit — call understand(symbol=%q) for the body",
		firstHitSymbol,
	)
}

// HintAfterDeadCode suggests get_file_health when the dead-symbol count
// in any single file crosses a threshold — that file is a probable
// hotspot worth scoring with biomarkers.
const deadCodeHotspotThreshold = 5

// HintAfterDeadCode returns a hint to call get_file_health when the worst
// offender file has more dead symbols than the hotspot threshold, or "" otherwise.
func HintAfterDeadCode(worstFile string, worstFileDeadCount int) string {
	if worstFileDeadCount < deadCodeHotspotThreshold || worstFile == "" {
		return ""
	}
	return fmt.Sprintf(
		"%d dead symbols in %s — call get_file_health(paths=[%q])",
		worstFileDeadCount, worstFile, worstFile,
	)
}

// declKeywords are Go declaration keywords that precede the symbol name.
// When one of these is the first word of a hit line body, the symbol name
// is the immediately following word.
var declKeywords = map[string]bool{
	"func":  true,
	"type":  true,
	"var":   true,
	"const": true,
}

// ExtractSymbolFromHit tries to parse "file.go:42:func Bar(...) {" → "Bar".
// Handles both declaration-prefixed forms ("func Bar(", "type Foo struct")
// and bare identifier lines. Returns "" on failure (which makes
// HintAfterCodeSearch go silent — safe).
func ExtractSymbolFromHit(hit string) string {
	parts := strings.SplitN(hit, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	body := strings.TrimSpace(parts[2])
	if body == "" {
		return ""
	}
	body = strings.TrimSuffix(body, " {")
	body = strings.TrimSuffix(body, "{")
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}

	// When the first token is a declaration keyword, the symbol name is the next token.
	if declKeywords[fields[0]] && len(fields) >= 2 {
		name := fields[1]
		// Strip trailing punctuation like "(" or ":".
		for _, c := range []string{"(", ":", "{"} {
			if i := strings.Index(name, c); i > 0 {
				name = name[:i]
			}
		}
		if name != "" {
			return name
		}
	}

	// Fallback: take the last token and strip trailing punctuation.
	last := fields[len(fields)-1]
	for _, c := range []string{"(", "{", ":"} {
		if i := strings.Index(last, c); i > 0 {
			last = last[:i]
		}
	}
	return last
}
