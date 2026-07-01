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

// ExtractSvelteWithRefs returns both the virtual TypeScript source (from the
// component's <script> blocks) and the capitalised JSX-style component tag
// references found in the template markup — the Svelte analogue of
// ExtractAstroWithRefs.
//
// scanTemplateRefs is template-syntax agnostic: a Svelte component has no ---
// frontmatter, so findTemplateStart returns 0 and the whole file is walked.
// <script>/<style> blocks and HTML comments are skipped; every opening tag whose
// name starts with an uppercase ASCII letter is recorded. Closing tags and
// namespace-prefixed tags (<svelte:head>, <svelte:component>, …) are skipped, so
// Svelte's own special elements do not produce spurious refs.
//
// Resolution of tag names to file paths is NOT performed here — callers should
// join TemplateRefs against the component's <script> imports for that purpose
// (see callgraph.ResolveTemplateRefs).
func ExtractSvelteWithRefs(src []byte) (*VirtualSource, []TemplateRef) {
	vs := ExtractSvelte(src)
	refs := scanTemplateRefs(src)
	return vs, refs
}
