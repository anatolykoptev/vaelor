// Package preproc extracts TypeScript-ish code blocks from preprocessor-language
// files (Svelte, Astro) into a "virtual source" buffer that can be fed to a
// tree-sitter TypeScript/TSX parser. The companion Builder type and per-extractor
// functions (ExtractSvelte, ExtractAstro) live in the same package.
//
// Import cycle note: this package does NOT import internal/parser. The
// RemapSymbolLines helper that rewrites parse results lives in
// internal/parser/preproc_remap.go (parser package) to avoid a cycle.
package preproc

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

// buildLineStartOffsets returns a slice of byte offsets where each line starts.
// Index 0 → byte 0 (start of first line). len(src)+1 is appended as sentinel.
func buildLineStartOffsets(src []byte) []int {
	offsets := []int{0}
	for i, c := range src {
		if c == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	offsets = append(offsets, len(src)+1) // sentinel
	return offsets
}
