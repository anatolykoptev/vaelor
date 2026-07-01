package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// astroHandler parses .astro files by extracting the leading --- frontmatter
// block and any <script> tags, then delegating to the TypeScript grammar.
// Symbol line numbers are remapped back to .astro file coordinates.
//
// Template body ({expr}) coverage:
//   - Component references (capitalised JSX tags, incl. those inside {expr}) are
//     captured as TemplateRefs (preproc.scanTemplateRefs, populated in Parse).
//   - Calls / member-calls / bare-identifier refs inside {expr} are captured as
//     CallSites via MarkupCalls (markup_calls.go), reached through ExtractCalls.
//
// Not supported (silently ignored, matches plan scope):
//   - Symbol (function/type/const) extraction from the template body — only the
//     frontmatter and <script> blocks contribute symbols.
//   - <style> blocks.
type astroHandler struct {
	parserBase
}

var astroLang = &astroHandler{}

func init() {
	// Set only the language name. Capabilities are borrowed lazily from tsLang
	// to avoid Go init-order issues (handler_astro.go < handler_typescript.go
	// alphabetically, so tsLang.caps is empty when this init runs).
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

// Parse extracts frontmatter + scripts, delegates to TS grammar, remaps lines,
// and populates TemplateRefs from capitalised tags in the template body.
func (h *astroHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	vs, refs := preproc.ExtractAstroWithRefs(src)
	result, err := parseWithTSAndRemap(path, vs, "astro", opts)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		result.TemplateRefs = refs
	}
	return result, nil
}
