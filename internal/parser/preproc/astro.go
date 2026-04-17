package preproc

import "bytes"

// ExtractAstro extracts the Astro frontmatter block (between leading ---) and
// all <script> blocks (page-level <script> tags). Returns VirtualSource with
// lang="astro".
//
// Handles:
//   - Leading frontmatter: starts with optional whitespace then ---\n, ends at
//     next line starting with ---
//   - Frontmatter may be absent — just <script> blocks processed
//   - Multiple <script> blocks with blank-line padding
//
// Does NOT handle:
//   - Template expressions {foo} in the HTML body
//   - Style tags
//   - Template literal strings containing <script> (backtick-quoted JS strings with
//     embedded <script> markers may be misidentified as real script blocks)
func ExtractAstro(src []byte) *VirtualSource {
	b := NewBuilder("astro")
	first := true

	pos := 0

	// ---- Frontmatter detection ----
	// Trim leading whitespace/blank lines to find if first non-empty content is ---
	trimmed := bytes.TrimLeft(src, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("---")) {
		// Find byte offset of the first --- in src.
		fmStart := bytes.Index(src, []byte("---"))
		// Content starts after the opening --- and the newline following it.
		afterDashes := fmStart + 3
		// Skip optional \r
		if afterDashes < len(src) && src[afterDashes] == '\r' {
			afterDashes++
		}
		// Skip the newline
		if afterDashes < len(src) && src[afterDashes] == '\n' {
			afterDashes++
		}

		// Find closing ---: a line that starts with ---
		// We search from afterDashes for \n--- (line beginning with ---)
		closeFM := findLinePrefix(src, afterDashes, []byte("---"))
		var fmEnd int
		if closeFM < 0 {
			fmEnd = len(src)
			pos = len(src)
		} else {
			fmEnd = closeFM
			// Advance pos past the closing --- line
			endOfCloseLine := bytes.IndexByte(src[closeFM:], '\n')
			if endOfCloseLine < 0 {
				pos = len(src)
			} else {
				pos = closeFM + endOfCloseLine + 1
			}
		}

		if fmEnd > afterDashes {
			b.AppendBlock(src, afterDashes, fmEnd)
			first = false
		}
	}

	// ---- <script> blocks ----
	for pos < len(src) {
		idx := bytes.Index(src[pos:], []byte("<script"))
		if idx < 0 {
			break
		}
		tagStart := pos + idx

		// Limit lookahead to one line or 512 bytes, whichever is shorter.
		tagEndLimit := tagStart + 512
		if tagEndLimit > len(src) {
			tagEndLimit = len(src)
		}
		if nl := bytes.IndexByte(src[tagStart:tagEndLimit], '\n'); nl >= 0 {
			// Allow '>' on the same line as the newline if it precedes it; cap at newline+1.
			tagEndLimit = tagStart + nl + 1
		}
		gtIdx := bytes.IndexByte(src[tagStart:tagEndLimit], '>')
		if gtIdx < 0 {
			break
		}
		contentStart := tagStart + gtIdx + 1

		closeTag := []byte("</script>")
		closeIdx := bytes.Index(src[contentStart:], closeTag)
		var contentEnd int
		if closeIdx < 0 {
			contentEnd = len(src)
		} else {
			contentEnd = contentStart + closeIdx
		}

		if !first {
			b.AppendBlankLine()
		}
		first = false

		b.AppendBlock(src, contentStart, contentEnd)

		if closeIdx < 0 {
			break
		}
		pos = contentEnd + len(closeTag)
	}

	return b.Build()
}

// findLinePrefix searches src[from:] for the first occurrence of a line that
// starts with prefix. Returns the byte offset in src of that line's start, or
// -1 if not found.
func findLinePrefix(src []byte, from int, prefix []byte) int {
	// Check if from itself is at a line start matching prefix.
	if from <= len(src) && bytes.HasPrefix(src[from:], prefix) {
		return from
	}
	// Search for \n followed by prefix.
	search := src[from:]
	for {
		nl := bytes.IndexByte(search, '\n')
		if nl < 0 {
			break
		}
		lineStart := nl + 1
		if lineStart <= len(search) && bytes.HasPrefix(search[lineStart:], prefix) {
			// Return absolute offset
			return from + (len(src[from:]) - len(search)) + lineStart
		}
		search = search[lineStart:]
		from += lineStart
	}
	return -1
}
