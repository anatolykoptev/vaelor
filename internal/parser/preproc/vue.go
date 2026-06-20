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
// The <script setup> block (if present) is appended first, followed by the
// plain <script> block. This mirrors Vue's compilation order and ensures
// setup-block symbols take precedence in the virtual buffer.
func ExtractVue(src []byte) *VirtualSource {
	b := NewBuilder("vue")
	// Append <script setup> first (composition API entry point).
	appendScriptBlocks(b, src, 0, true)
	return b.Build()
}
