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
// Supported (Svelte 5):
//   - Rune call expressions are detected after TS parsing and emitted as KindRune
//     symbols with a RuneKind field set to the canonical category. Canonical list
//     (sourced from Svelte 5 compiler CallExpression visitor):
//     state:    $state, $state.raw, $state.eager, $state.snapshot
//     derived:  $derived, $derived.by
//     effect:   $effect, $effect.pre, $effect.tracking, $effect.root, $effect.pending
//     props:    $props, $props.id
//     bindable: $bindable
//     inspect:  $inspect, $inspect.trace
//     host:     $host
//   - NOT classified as runes: $$slots/$$props/$$restProps (Svelte 4 legacy),
//     $.proxy/$.computed/etc. (Svelte 5 internals), $inspect.with (chained method).
//
// Not supported (silently ignored, matches plan scope):
//   - Template markup ({#if}, {#each}, <slot>, component invocations)
//   - <style> blocks
//   - Destructured $props() bindings (e.g. let { name } = $props() — skipped,
//     known limitation; the standalone $props() call is still emitted)
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

// Parse extracts <script> blocks, delegates to the TypeScript parser, remaps
// symbol line numbers back to the original .svelte file coordinates, then
// appends Svelte 5 rune symbols detected by the post-parse rune classifier.
func (h *svelteHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	vs := preproc.ExtractSvelte(src)
	result, err := parseWithTSAndRemap(path, vs, "svelte", opts)
	if err != nil {
		return nil, err
	}
	appendRuneSymbols(result, vs, path)
	return result, nil
}
