package coupling

import (
	"fmt"
	"regexp"
)

const (
	// minTokenLen / maxTokenLen bound a significant token: shorter than min is
	// too generic to prove coupling; longer than max is almost always a hash,
	// base64 blob, or generated id, not a shared contract name.
	minTokenLen = 5
	maxTokenLen = 48
)

// screamingRe matches SCREAMING_SNAKE_CASE tokens with at least one underscore:
// env-var names, shared consts (RELAY_JWT_SECRET, MAX_PEERS). The required
// underscore excludes single words like GET/POST/ERROR by construction.
var screamingRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)

// literalRe matches the CONTENTS of single/double/back-quoted string literals.
// Group 1 captures the inner text; significance is decided in addToken. The
// {min,max} quantifier is built from the named length consts (single source of
// truth) — note this enforces length on the LITERAL path at match time, which
// is why addToken's own length gate is load-bearing only for the screaming path.
// Note: no escape handling — an escaped quote splits the literal; a partial
// fragment is harmless noise downstream (intersection absorbs it).
var literalRe = regexp.MustCompile(
	fmt.Sprintf("[\"'`]([^\"'`\\n]{%d,%d})[\"'`]", minTokenLen, maxTokenLen))

// structuredLiteralRe accepts a string-literal value only if it looks like a
// protocol identifier: lowercase snake_case or kebab-case with ≥1 separator
// (peer_joined, ice-candidate). Bare words (offer, answer) are rejected.
var structuredLiteralRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[_-][a-z0-9]+)+$`)

// stopTokens are multi-segment tokens that recur across unrelated services and
// therefore prove nothing even when shared. Kept deliberately SMALL — structural
// rarity (required separator) + cross-file intersection do most of the filtering.
var stopTokens = map[string]bool{
	"content_type":     true,
	"content-type":     true,
	"CONTENT_TYPE":     true,
	"application_json": true,
	"application-json": true,
	"user_agent":       true,
	"user-agent":       true,
	"USER_AGENT":       true,
	"status_code":      true,
	"STATUS_CODE":      true,
}

// extractSignificantSymbols returns the set of high-signal tokens in src:
// SCREAMING_SNAKE consts/env-vars and structured (snake/kebab) string literals,
// minus the stop-set. Language-agnostic — operates on raw bytes, so it works
// identically across Rust/TS/Svelte/Go. No I/O, no CGO.
func extractSignificantSymbols(src []byte) map[string]struct{} {
	out := make(map[string]struct{})
	s := string(src)

	for _, m := range screamingRe.FindAllString(s, -1) {
		addToken(out, m, false)
	}
	for _, m := range literalRe.FindAllStringSubmatch(s, -1) {
		addToken(out, m[1], true)
	}
	return out
}

// addToken inserts tok if it passes significance. structured=true requires the
// snake/kebab shape (for string literals); screaming tokens already proved their
// structure via the required underscore in screamingRe.
func addToken(out map[string]struct{}, tok string, structured bool) {
	// Length gate. Redundant for the literal path (literalRe already enforces
	// {minTokenLen,maxTokenLen} at match time) but the ONLY length bound for
	// SCREAMING tokens — screamingRe has no length limit. Do not remove.
	if len(tok) < minTokenLen || len(tok) > maxTokenLen {
		return
	}
	if stopTokens[tok] {
		return
	}
	if structured && !structuredLiteralRe.MatchString(tok) {
		return
	}
	out[tok] = struct{}{}
}
