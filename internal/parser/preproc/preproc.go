// Package preproc extracts TypeScript-ish code blocks from preprocessor-language
// files (Svelte, Astro) into a "virtual source" buffer that can be fed to a
// tree-sitter TypeScript/TSX parser. The companion Builder type and per-extractor
// functions (ExtractSvelte, ExtractAstro) live in the same package.
//
// Import cycle note: this package does NOT import internal/parser. The
// RemapSymbolLines helper that rewrites parse results lives in
// internal/parser/preproc_remap.go (parser package) to avoid a cycle.
package preproc

// tagOpenScanLimit is the maximum number of bytes the scanner will look ahead
// from the start of a '<script' token when searching for the closing '>'.
// This bounds the scan on malformed tags that lack a closing '>' and have no
// newline — without it, the scanner would traverse the rest of the file.
const tagOpenScanLimit = 512

// TemplateRef records a single usage of a capitalised JSX-style component tag
// found in an Astro (or similar) template body.
//
// Name is the bare tag name (e.g. "Breadcrumbs"), Line and Col are 1-based
// positions in the original source file.
// Closing tags (</Foo>) are not recorded — only opening tags.
type TemplateRef struct {
	Name string
	Line uint32
	Col  uint32
}

// VirtualSource is TypeScript code extracted from a preprocessor-language
// file (Svelte, Astro, Vue) together with a per-virtual-line mapping back
// to the original source line numbers.
type VirtualSource struct {
	// Code is the synthetic TypeScript buffer passed to the tree-sitter parser.
	Code []byte

	// Lang is the effective language label for Symbol.Language ("svelte", "astro", …).
	Lang string

	// LineMap[i] is the 1-based line number in the ORIGINAL source that corresponds
	// to line (i+1) in Code. Length == number of \n in Code + 1. Lines that exist
	// only in Code (blank padding between script blocks) map to 0.
	LineMap []uint32
}

// countLines returns the number of lines in b (== number of '\n' bytes + 1).
// An empty slice has 1 line (the empty line itself).
func countLines(b []byte) int {
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
