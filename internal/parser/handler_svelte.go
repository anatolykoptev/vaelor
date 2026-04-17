package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// svelteHandler parses .svelte single-file components by extracting their
// <script> and <script context="module"> blocks and delegating to the TypeScript
// tree-sitter grammar. Symbol line numbers are remapped back to the .svelte
// file's coordinates via preproc.RemapSymbolLines.
//
// Not supported (silently ignored, matches plan scope):
//   - Template markup ({#if}, {#each}, <slot>, component invocations)
//   - <style> blocks
//   - Svelte 5 runes as first-class symbols (parsed as plain function calls)
type svelteHandler struct {
	parserBase
}

var svelteLang = &svelteHandler{}

func init() {
	// Set only the language name. Capabilities are borrowed lazily from tsLang
	// to avoid Go init-order issues (handler_svelte.go < handler_typescript.go
	// alphabetically, so tsLang.caps is empty when this init runs).
	svelteLang.parserBase = parserBase{lang: "svelte"}
	registerHandler(svelteLang)
}

func (h *svelteHandler) Extensions() []string { return []string{".svelte"} }

// Capabilities delegates to TypeScript's capabilities — <script> blocks are
// parsed with the TS grammar and queries. Called lazily, after all inits.
func (h *svelteHandler) Capabilities() Capabilities { return tsLang.Capabilities() }

// MapCapture delegates to the TypeScript capture mapper since <script> blocks are TypeScript.
func (h *svelteHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}

// Parse extracts <script> blocks, delegates to the TypeScript parser, then
// remaps symbol line numbers back to the original .svelte file coordinates.
func (h *svelteHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	return parseWithTSAndRemap(path, preproc.ExtractSvelte(src), "svelte", opts)
}
