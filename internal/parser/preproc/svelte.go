package preproc

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
	appendScriptBlocks(b, src, 0, true)
	return b.Build()
}
