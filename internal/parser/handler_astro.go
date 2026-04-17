package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// astroHandler parses .astro files by extracting the leading --- frontmatter
// block and any <script> tags, then delegating to the TypeScript grammar.
// Symbol line numbers are remapped back to .astro file coordinates.
//
// Not supported (silently ignored, matches plan scope):
//   - Template expressions in the HTML body ({foo}, {...})
//   - <style> blocks
//   - HTML/JSX markup between frontmatter and scripts
type astroHandler struct {
	parserBase
}

var astroLang = &astroHandler{}

func init() {
	astroLang.parserBase = parserBase{lang: "astro"}
	registerHandler(astroLang)
}

func (h *astroHandler) Extensions() []string { return []string{".astro"} }

// Capabilities delegates to TypeScript — frontmatter and <script> contents are TS.
func (h *astroHandler) Capabilities() Capabilities { return tsLang.Capabilities() }

// MapCapture delegates to the TypeScript capture mapper.
func (h *astroHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}

// Parse extracts frontmatter + scripts, delegates to TS grammar, remaps lines.
func (h *astroHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	return parseWithTSAndRemap(path, preproc.ExtractAstro(src), "astro", opts)
}
