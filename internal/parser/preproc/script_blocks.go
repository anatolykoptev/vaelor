package preproc

import "bytes"

// appendScriptBlocks scans src starting at pos for <script> … </script> blocks
// and appends each block's CONTENT to b, separated by blank lines. The `first`
// flag indicates whether b already holds content (e.g. Astro frontmatter that
// was appended before the script scan) — when false, the first script block gets
// a leading blank-line separator.
//
// It is the shared byte-walker behind ExtractSvelte and the <script> half of
// ExtractAstro. Behaviour notes (apply to both callers):
//   - case-sensitive "<script" match (Svelte/Astro are case-sensitive)
//   - opening-tag '>' lookahead capped at one line or tagOpenScanLimit bytes
//   - missing </script> → treat content as extending to EOF (best-effort)
//   - CRLF and LF line endings handled by the byte indexing
func appendScriptBlocks(b *Builder, src []byte, pos int, first bool) {
	for pos < len(src) {
		// Find next <script (case-sensitive).
		idx := bytes.Index(src[pos:], []byte("<script"))
		if idx < 0 {
			break
		}
		tagStart := pos + idx

		// Find the closing '>' of the opening tag.
		// Limit lookahead to one line or tagOpenScanLimit bytes, whichever is shorter.
		tagEndLimit := tagStart + tagOpenScanLimit
		if tagEndLimit > len(src) {
			tagEndLimit = len(src)
		}
		if nl := bytes.IndexByte(src[tagStart:tagEndLimit], '\n'); nl >= 0 {
			// Allow '>' on the same line as the newline if it precedes it; cap at newline+1.
			tagEndLimit = tagStart + nl + 1
		}
		gtIdx := bytes.IndexByte(src[tagStart:tagEndLimit], '>')
		if gtIdx < 0 {
			break // malformed
		}
		contentStart := tagStart + gtIdx + 1

		// Find </script>.
		closeTag := []byte("</script>")
		closeIdx := bytes.Index(src[contentStart:], closeTag)
		var contentEnd int
		if closeIdx < 0 {
			contentEnd = len(src) // best-effort: treat EOF as close
		} else {
			contentEnd = contentStart + closeIdx
		}

		// Blank-line separator between blocks.
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
}
