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
		gtIdx := bytes.IndexByte(src[tagStart:], '>')
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
