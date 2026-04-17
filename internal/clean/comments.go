package clean

import (
	"strings"
)

// cStyleLanguages is the set of languages that use // and /* */ comment syntax.
// svelte / astro scripts are TypeScript/JavaScript so they use the same style.
var cStyleLanguages = map[string]bool{
	"go": true, "javascript": true, "typescript": true, "java": true,
	"rust": true, "c": true, "cpp": true, "csharp": true,
	"swift": true, "kotlin": true,
	"svelte": true, "astro": true,
}

// hashStyleLanguages is the set of languages that use # comment syntax.
var hashStyleLanguages = map[string]bool{
	"python": true, "ruby": true, "shell": true, "yaml": true, "toml": true,
}

// cStylePreserveKeywords are keywords that mark a comment as worth keeping.
var cStylePreserveKeywords = []string{
	"TODO", "FIXME", "HACK", "NOTE", "BUG", "XXX",
	"nolint", "eslint-disable", "prettier-ignore", "nosec",
}

// hashStylePreserveKeywords are keywords that mark a # comment as worth keeping.
var hashStylePreserveKeywords = []string{
	"TODO", "FIXME", "HACK", "NOTE", "BUG", "XXX",
	"noqa", "type:ignore",
}

// shouldPreserve reports whether a comment text contains any preservation keyword.
func shouldPreserve(comment string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(comment, kw) {
			return true
		}
	}
	return false
}

// hasOddQuotesBefore reports whether the number of double-quote characters in
// s before index idx is odd, which is a heuristic for "we're inside a string
// literal".  Single quotes are deliberately ignored to keep the heuristic
// simple and avoid false positives in languages like Go where rune literals
// with a single quote are uncommon adjacent to comment markers.
func hasOddQuotesBefore(s string, idx int) bool {
	count := 0
	for i := range idx {
		if s[i] == '"' {
			count++
		}
	}
	return count%2 == 1
}

// stripComments removes comments from source according to the language family.
// Lines that contain preservation keywords are kept as-is.
func stripComments(source, language string) string {
	switch {
	case cStyleLanguages[language]:
		return stripCStyleComments(source)
	case hashStyleLanguages[language]:
		return stripHashStyleComments(source)
	default:
		return source
	}
}

// stripCStyleComments handles // single-line and /* */ block comments.
func stripCStyleComments(source string) string {
	lines := strings.Split(source, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false

	for _, line := range lines {
		stripped, keep := processCStyleLine(line, &inBlock)
		if keep {
			out = append(out, stripped)
		}
	}

	return strings.Join(out, "\n")
}

// processCStyleLine processes a single line for C-style comment stripping.
// It updates the inBlock state and returns the (possibly modified) line and
// whether it should be included in the output.
func processCStyleLine(line string, inBlock *bool) (result string, keep bool) {
	trimmed := strings.TrimSpace(line)

	// Handle doc comments: /// and /** — always preserve.
	if strings.HasPrefix(trimmed, "///") || strings.HasPrefix(trimmed, "/**") {
		if *inBlock {
			// Unusual but treat /** inside a block as continuing the block.
			return "", false
		}
		return line, true
	}

	// If inside a block comment, look for the closing */.
	if *inBlock {
		if idx := strings.Index(line, "*/"); idx >= 0 {
			*inBlock = false
			remainder := line[idx+2:]
			// If there is meaningful content after */, keep that part.
			if strings.TrimSpace(remainder) != "" {
				return remainder, true
			}
		}
		return "", false
	}

	// Not in a block comment. Scan the line character by character to find
	// comment markers while respecting string literals.
	return scanCStyleLine(line, inBlock)
}

// scanCStyleLine scans a non-block-comment line and strips comment portions.
// Returns the cleaned line text and whether to include it.
func scanCStyleLine(line string, inBlock *bool) (string, bool) {
	for i := 0; i < len(line)-1; i++ {
		ch := line[i]
		if ch == '\\' {
			i++ // skip next char (escape sequence)
			continue
		}
		if ch != '/' {
			continue
		}
		next := line[i+1]
		if next == '/' {
			if hasOddQuotesBefore(line, i) {
				continue
			}
			return handleLineComment(line, i)
		}
		if next == '*' {
			if hasOddQuotesBefore(line, i) {
				continue
			}
			return handleBlockCommentStart(line, i, inBlock)
		}
	}
	return line, true
}

// handleLineComment processes a // comment found at position commentIdx.
func handleLineComment(line string, commentIdx int) (string, bool) {
	commentText := line[commentIdx:]
	// Keep /// doc comments.
	if strings.HasPrefix(strings.TrimSpace(commentText), "///") {
		return line, true
	}
	if shouldPreserve(commentText, cStylePreserveKeywords) {
		return line, true
	}
	before := strings.TrimRight(line[:commentIdx], " \t")
	if before != "" {
		return before, true
	}
	return "", false
}

// handleBlockCommentStart processes a /* found at position startIdx.
// It either strips it (updating inBlock if needed) or preserves the line.
func handleBlockCommentStart(line string, startIdx int, inBlock *bool) (string, bool) {
	blockText := line[startIdx:]
	closeIdx := strings.Index(blockText[2:], "*/")
	if closeIdx >= 0 {
		return handleInlineBlockComment(line, startIdx, closeIdx+2)
	}
	// Block does not close on this line.
	*inBlock = true
	if shouldPreserve(blockText, cStylePreserveKeywords) {
		return line, true
	}
	before := strings.TrimRight(line[:startIdx], " \t")
	if before != "" {
		return before, true
	}
	return "", false
}

// handleInlineBlockComment handles /* ... */ that opens and closes on one line.
// closeOffset is the index of */ relative to startIdx (already adjusted for the
// 2-char "/*" prefix skip).
func handleInlineBlockComment(line string, startIdx, closeOffset int) (string, bool) {
	comment := line[startIdx : startIdx+closeOffset+2]
	if shouldPreserve(comment, cStylePreserveKeywords) {
		return line, true
	}
	before := strings.TrimRight(line[:startIdx], " \t")
	after := line[startIdx+closeOffset+2:]
	rebuilt := before + after
	if strings.TrimSpace(rebuilt) == "" {
		return "", false
	}
	return rebuilt, true
}

// stripHashStyleComments handles # single-line comments for Python, Ruby, etc.
func stripHashStyleComments(source string) string {
	lines := strings.Split(source, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		stripped, keep := processHashStyleLine(line)
		if keep {
			out = append(out, stripped)
		}
	}

	return strings.Join(out, "\n")
}

// processHashStyleLine strips a # comment from a single line if appropriate.
func processHashStyleLine(line string) (result string, keep bool) {
	idx := strings.Index(line, "#")
	if idx < 0 {
		return line, true
	}

	// Heuristic: if there is an odd number of " before the #, we may be
	// inside a string literal.
	if hasOddQuotesBefore(line, idx) {
		return line, true
	}

	commentText := line[idx:]
	before := strings.TrimRight(line[:idx], " \t")

	if shouldPreserve(commentText, hashStylePreserveKeywords) {
		return line, true
	}

	if before != "" {
		return before, true
	}
	return "", false
}
