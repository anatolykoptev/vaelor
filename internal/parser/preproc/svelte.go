package preproc

import "bytes"

// ExtractSvelte extracts all <script> and <script context="module"> /
// <script module> blocks from a Svelte component and returns a VirtualSource
// with lang="svelte".
//
// Handles:
//   - <script> … </script>
//   - <script lang="ts"> … </script>
//   - <script context="module"> … </script>  (Svelte 4)
//   - <script module> … </script>            (Svelte 5)
//   - Multiple script tags — concatenated with blank-line padding
//   - CRLF and LF line endings
//   - Missing closing tag — treat as extending to EOF (best-effort)
//
// Does NOT handle:
//   - Script content inside HTML comments
//   - <style> blocks
//   - Template markup
//   - Escaped &lt;script&gt; in string literals
//   - Template literal strings containing <script> (backtick-quoted JS strings with
//     embedded <script> markers may be misidentified as real script blocks)
func ExtractSvelte(src []byte) *VirtualSource {
	b := NewBuilder("svelte")
	first := true

	pos := 0
	for pos < len(src) {
		// Find next <script (case-sensitive, Svelte is case-sensitive)
		idx := bytes.Index(src[pos:], []byte("<script"))
		if idx < 0 {
			break
		}
		tagStart := pos + idx

		// Find the closing '>' of the opening tag.
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
			break // malformed
		}
		contentStart := tagStart + gtIdx + 1

		// Find </script>
		closeTag := []byte("</script>")
		closeIdx := bytes.Index(src[contentStart:], closeTag)
		var contentEnd int
		if closeIdx < 0 {
			contentEnd = len(src) // best-effort: treat EOF as close
		} else {
			contentEnd = contentStart + closeIdx
		}

		// Add blank-line separator between blocks.
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
