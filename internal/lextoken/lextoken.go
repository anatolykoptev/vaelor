// Package lextoken provides the canonical query tokenizer and identifier
// splitter for go-code. It is a leaf package (stdlib-only, no internal
// imports) consumed by both internal/analyze and internal/embeddings.
//
// Three previously divergent sites are collapsed here:
//   - splitIdentifier/splitCamelCase (analyze/context.go) → SplitIdentifier/SplitCamelCase
//   - extractQueryTerms (analyze/context.go) → Tokenize
//   - extractKeywordsForBoost (analyze/rank.go) and ExtractQueryKeywords
//     (embeddings/store_keyword.go) — byte-identical logic → KeywordTokenize
//
// Stopword decision: analyze/rank.go and embeddings/store_keyword.go had
// byte-identical stopword sets. They are unified into one canonical set here.
// coupling/symbols.go:stopTokens is a different domain (HTTP-literal dedup)
// and is NOT touched by this package.
package lextoken

import (
	"regexp"
	"strings"
	"unicode"
)

// nonAlphanumRe matches characters that are not letters, digits, or underscores.
// Mirrors the original at analyze/context.go:135.
var nonAlphanumRe = regexp.MustCompile(`[^\w]`)

// stopwords is the canonical English + code-domain stopword set.
// Derived from the byte-identical sets in analyze/rank.go:287 and
// embeddings/store_keyword.go:77. Union is safe: both sets were identical.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "with": true,
	"this": true, "from": true, "are": true, "not": true, "have": true,
	"function": true, "method": true, "code": true, "file": true,
	"which": true, "where": true, "when": true, "how": true, "what": true,
}

// IsStopword reports whether word is in the canonical stopword set.
func IsStopword(word string) bool {
	return stopwords[word]
}

// FilterStopwords returns a copy of terms with stopwords removed.
// Returns nil when input is nil (preserves nil-vs-empty distinction).
func FilterStopwords(terms []string) []string {
	if terms == nil {
		return nil
	}
	out := terms[:0:0] // zero-len, zero-cap — avoids aliasing the input slice
	for _, t := range terms {
		if !stopwords[t] {
			out = append(out, t)
		}
	}
	return out
}

// isCamelBoundary reports whether position i in runes is a camelCase split
// point. Mirrors analyze/context.go:167.
func isCamelBoundary(runes []rune, i int) bool {
	prev, cur := runes[i-1], runes[i]
	if unicode.IsLower(prev) && unicode.IsUpper(cur) {
		return true
	}
	if unicode.IsLetter(prev) && unicode.IsDigit(cur) {
		return true
	}
	if unicode.IsDigit(prev) && unicode.IsLetter(cur) {
		return true
	}
	if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
		return true
	}
	return false
}

// SplitCamelCase splits a camelCase or PascalCase identifier into lowercase
// subwords. Subwords shorter than 2 characters are dropped.
// Mirrors analyze/context.go:138 (splitCamelCase).
func SplitCamelCase(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	runes := []rune(s)
	start := 0

	for i := 1; i < len(runes); i++ {
		if isCamelBoundary(runes, i) {
			part := strings.ToLower(string(runes[start:i]))
			if len(part) >= 2 { //nolint:mnd // minimum subword length
				parts = append(parts, part)
			}
			start = i
		}
	}

	if start < len(runes) {
		part := strings.ToLower(string(runes[start:]))
		if len(part) >= 2 { //nolint:mnd // minimum subword length
			parts = append(parts, part)
		}
	}

	return parts
}

// SplitIdentifier splits an identifier on underscores, then splits each part
// by camelCase. Empty parts (from leading/trailing/double underscores) are
// skipped. Mirrors analyze/context.go:186 (splitIdentifier).
func SplitIdentifier(s string) []string {
	snakeParts := strings.Split(s, "_")
	var result []string

	for _, part := range snakeParts {
		if part == "" {
			continue
		}
		camelParts := SplitCamelCase(part)
		result = append(result, camelParts...)
	}

	return result
}

// Tokenize splits a natural-language or identifier query into lowercase
// alphanumeric terms for symbol matching. It:
//  1. Lowercases and strips non-alphanumeric characters from each word.
//  2. Keeps terms ≥ 3 characters.
//  3. Also runs identifier splitting (SplitIdentifier) on each word, adding
//     the resulting subwords as additional terms.
//  4. Deduplicates across both passes.
//
// NOTE: Tokenize does NOT filter stopwords — it is used in analyze/context.go
// where stopwords are intentional query signals (e.g. "what", "are").
// Use KeywordTokenize for stopword-filtered keyword extraction.
//
// Mirrors analyze/context.go:202 (extractQueryTerms).
func Tokenize(query string) []string {
	seen := make(map[string]struct{})
	var terms []string

	addTerm := func(t string) {
		if _, ok := seen[t]; !ok && len(t) >= 3 { //nolint:mnd // minimum term length to avoid noise
			seen[t] = struct{}{}
			terms = append(terms, t)
		}
	}

	rawWords := strings.Fields(query)

	// First pass: whole-word lowercase-cleaned tokens.
	for _, raw := range rawWords {
		lower := strings.ToLower(raw)
		cleaned := nonAlphanumRe.ReplaceAllString(lower, "")
		if len(cleaned) >= 3 { //nolint:mnd // minimum term length to avoid noise
			addTerm(cleaned)
		}
	}

	// Second pass: identifier-split subwords.
	for _, raw := range rawWords {
		cleaned := nonAlphanumRe.ReplaceAllString(raw, "")
		if len(cleaned) < 3 { //nolint:mnd // minimum term length to avoid noise
			continue
		}
		subwords := SplitIdentifier(cleaned)
		for _, sw := range subwords {
			addTerm(sw)
		}
	}

	return terms
}

// KeywordTokenize splits a natural-language query into meaningful search terms
// by:
//  1. Lowercasing and splitting on non-alphanumeric characters.
//  2. Keeping terms ≥ 3 characters.
//  3. Filtering canonical stopwords (IsStopword).
//  4. Deduplicating.
//
// It does NOT perform identifier splitting (use Tokenize for that).
// Mirrors the byte-identical logic from analyze/rank.go:286
// (extractKeywordsForBoost) and embeddings/store_keyword.go:76
// (ExtractQueryKeywords).
func KeywordTokenize(query string) []string {
	seen := make(map[string]bool)
	var keywords []string
	for _, word := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(word) >= 3 && !stopwords[word] && !seen[word] { //nolint:mnd // minimum term length to avoid noise
			seen[word] = true
			keywords = append(keywords, word)
		}
	}
	return keywords
}
