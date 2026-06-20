package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// vueHandler parses .vue single-file components (SFCs) by extracting their
// <script setup> and <script> blocks and delegating to the TypeScript
// tree-sitter grammar. Symbol line numbers are remapped back to the .vue
// file's coordinates via preproc.RemapSymbolLines.
//
// Supported:
//   - <script setup> … </script>         (Composition API, Vue 3)
//   - <script setup lang="ts"> … </script>
//   - <script> … </script>               (Options API or plain script)
//   - <script lang="ts"> … </script>
//   - Both blocks in the same SFC — both contribute symbols
//
// Not supported (silently ignored, matches plan scope):
//   - <template> expressions and component references
//   - <style> blocks
type vueHandler struct {
	parserBase
}

var vueLang = &vueHandler{}

func init() {
	// Set only the language name. Capabilities are borrowed lazily from tsLang
	// to avoid Go init-order issues (handler_vue.go < handler_typescript.go
	// alphabetically, so tsLang.caps is empty when this init runs).
	vueLang.parserBase = parserBase{lang: "vue"}
	registerHandler(vueLang)
}

func (h *vueHandler) Extensions() []string { return []string{".vue"} }

// Capabilities delegates to TypeScript's capabilities — <script> blocks are
// parsed with the TS grammar and queries. Called lazily, after all inits.
func (h *vueHandler) Capabilities() Capabilities { return tsLang.Capabilities() }

// MapCapture delegates to the TypeScript capture mapper since <script> blocks are TypeScript.
func (h *vueHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}

// Parse extracts <script setup> and <script> blocks, delegates to the TypeScript
// parser, and remaps symbol line numbers back to the original .vue file coordinates.
func (h *vueHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	vs := preproc.ExtractVue(src)
	result, err := parseWithTSAndRemap(path, vs, "vue", opts)
	if err != nil {
		return nil, err
	}
	return result, nil
}
