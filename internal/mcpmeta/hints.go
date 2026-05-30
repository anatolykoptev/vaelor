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

// explainQueryRE recognises English explain-class queries that want prose, not chaining.
// Go's RE2 \b is ASCII-only, so Cyrillic verbs are matched by explainQueryRURE instead.
var explainQueryRE = regexp.MustCompile(`(?i)\b(why|how|describe|explain)\b`)

// explainQueryRURE recognises Russian explain-class queries.
// Uses simple substring match — Cyrillic words are unambiguous enough in practice.
var explainQueryRURE = regexp.MustCompile(`(?i)(почему|как|опиши|объясни|расскажи)`)

// HintAfterCodeSearch returns a calibrated chaining hint for a code_search
// response, or "" when no hint is warranted.
//
// Rules:
//   - explain-class query → silent (caller wants prose, not chaining)
//   - exactly 1 hit → suggest understand(symbol=...) on that symbol
//   - 0 or >1 hits → silent (too many to pin one symbol)
func HintAfterCodeSearch(query string, nHits int, firstHitSymbol string) string {
	if explainQueryRE.MatchString(query) || explainQueryRURE.MatchString(query) {
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

// goIdentRE matches a valid Go identifier (exported or unexported).
var goIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// declKeywords are Go declaration keywords that precede the symbol name.
var declKeywords = []string{"func", "type", "var", "const"}

// ExtractSymbolFromHit returns the identifier from a declaration-style match
// line like "foo.go:42:func Bar(...)". Returns "" for non-declaration lines
// (call sites, string literals, comments, control flow) so HintAfterCodeSearch
// stays silent rather than suggesting a bogus understand(symbol=...) call.
func ExtractSymbolFromHit(hit string) string {
	parts := strings.SplitN(hit, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	body := strings.TrimSpace(parts[2])

	// Must start with one of the declaration keywords.
	var rest string
	for _, kw := range declKeywords {
		if strings.HasPrefix(body, kw+" ") {
			rest = strings.TrimSpace(body[len(kw):])
			break
		}
	}
	if rest == "" {
		return ""
	}

	// Take the first whitespace-separated token, strip trailing `(`/`{`/`:`.
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	sym := fields[0]
	for _, c := range []string{"(", "{", ":"} {
		if i := strings.Index(sym, c); i > 0 {
			sym = sym[:i]
		}
	}

	// Must match a Go identifier shape.
	if !goIdentRE.MatchString(sym) {
		return ""
	}
	return sym
}
