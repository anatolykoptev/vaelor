package preproc

// ExtractVue extracts <script setup> and <script> blocks from a Vue single-file
// component (SFC) and returns a VirtualSource with lang="vue".
//
// Handles:
//   - <script setup> … </script>
//   - <script setup lang="ts"> … </script>
//   - <script> … </script>
//   - <script lang="ts"> … </script>
//   - Both blocks in the same SFC — concatenated with blank-line padding
//   - CRLF and LF line endings
//   - Missing </script> — treats content as extending to EOF (best-effort)
//
// Does NOT handle:
//   - <template> block expressions or component references
//   - <style> blocks
//   - Script content inside HTML comments
//   - Escaped &lt;script&gt; inside string literals
//
// Both blocks are appended in document (source) order: <script setup> first
// because appendScriptBlocks starts scanning from offset 0, so the earlier
// block in the file appears first in the virtual buffer. In practice Vue SFCs
// place <script setup> before <script>, which matches this behaviour.
func ExtractVue(src []byte) *VirtualSource {
	b := NewBuilder("vue")
	// Noted: <script setup> macros defineProps/defineEmits/withDefaults produce
	// spurious callee symbols via TS grammar call-expr; defer to G3 cleanup.
	appendScriptBlocks(b, src, 0, true)
	return b.Build()
}
