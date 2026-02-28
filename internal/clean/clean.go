// Package clean provides smart code cleaning for LLM consumption.
//
// Cleaning transforms raw source code into a compact representation suitable
// for feeding into LLM context windows while preserving semantic meaning.
// Strategies include: comment stripping (preserving doc comments and TODOs),
// whitespace normalization, base64/binary content truncation, and boilerplate
// removal for well-known patterns (auto-generated files, test data, etc.).
package clean

import (
	"strings"
	"unicode/utf8"
)

// CleanOpts controls which cleaning transformations are applied.
type CleanOpts struct {
	// StripComments removes inline and block comments.
	// Doc comments (/** ... */ or // ...) immediately before exported symbols are preserved.
	StripComments bool

	// StripBlankLines collapses consecutive blank lines into a single blank line.
	StripBlankLines bool

	// TruncateLongLines shortens lines longer than MaxLineChars by removing the tail.
	TruncateLongLines bool

	// MaxLineChars is the line length limit when TruncateLongLines is true.
	MaxLineChars int

	// TruncateBase64 shortens base64 or hex string literals that exceed 80 chars.
	TruncateBase64 bool

	// MaxFileChars is the total character limit for the output.
	// Content is truncated with a marker if it exceeds this limit. 0 = no limit.
	MaxFileChars int
}

// DefaultOpts returns sensible defaults for LLM-targeted cleaning.
func DefaultOpts() CleanOpts {
	const defaultMaxLineChars = 500
	const defaultMaxFileChars = 50000

	return CleanOpts{
		StripComments:     true,
		StripBlankLines:   true,
		TruncateLongLines: true,
		MaxLineChars:      defaultMaxLineChars,
		TruncateBase64:    true,
		MaxFileChars:      defaultMaxFileChars,
	}
}

// CleanSource applies the configured transformations to the source code string
// and returns the cleaned version.
//
// Transformations are applied in this order:
//  1. Comment stripping (language-specific)
//  2. Blank line collapsing (run of blanks → single blank)
//  3. Long line truncation
//  4. File-level truncation
func CleanSource(source, language string, opts CleanOpts) string {
	if !utf8.ValidString(source) {
		return "[binary or invalid UTF-8 content omitted]"
	}

	result := source

	// Strip comments first so that comment-only lines become blank and are
	// then collapsed in the next step.
	if opts.StripComments {
		result = stripComments(result, language)
	}

	if opts.StripBlankLines {
		result = collapseBlankLines(result)
	}

	if opts.TruncateLongLines && opts.MaxLineChars > 0 {
		result = truncateLongLines(result, opts.MaxLineChars)
	}

	if opts.MaxFileChars > 0 && utf8.RuneCountInString(result) > opts.MaxFileChars {
		// Truncate at a rune boundary.
		byteOffset := 0
		for j := 0; j < opts.MaxFileChars; j++ {
			_, size := utf8.DecodeRuneInString(result[byteOffset:])
			byteOffset += size
		}
		result = result[:byteOffset] + "\n... [truncated]\n"
	}

	return result
}

// collapseBlankLines reduces runs of more than one blank line to a single blank line.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, line)
			}
		} else {
			blankRun = 0
			out = append(out, line)
		}
	}

	return strings.Join(out, "\n")
}

// truncateLongLines shortens any line longer than maxChars runes by cutting it
// at a rune boundary and appending a marker.
func truncateLongLines(s string, maxChars int) string {
	const truncMarker = " …"
	lines := strings.Split(s, "\n")

	for i, line := range lines {
		if utf8.RuneCountInString(line) > maxChars {
			// Advance rune-by-rune to find the byte offset of maxChars runes.
			byteOffset := 0
			for j := 0; j < maxChars; j++ {
				_, size := utf8.DecodeRuneInString(line[byteOffset:])
				byteOffset += size
			}
			lines[i] = line[:byteOffset] + truncMarker
		}
	}

	return strings.Join(lines, "\n")
}
