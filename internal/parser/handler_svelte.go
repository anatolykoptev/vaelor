package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
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
//   - Destructured $props() bindings (let { name, count } = $props()) — each bound
//     name is emitted as a KindRune/props symbol, in addition to the $props token
//     symbol, so individual props are discoverable by their own identifier.
//   - Component-composition refs: capitalised JSX-style tags in the markup
//     (<Card/>) are captured as TemplateRefs (preproc.scanTemplateRefs via
//     ExtractSvelteWithRefs), mirroring Astro. callgraph.ResolveTemplateRefs joins
//     them against the component's <script> imports to emit file-level USES edges.
//   - Template-expression calls/refs: plain {expr} mustaches AND the header EXPR of
//     control-flow / special tags ({#if EXPR}, {#each EXPR as x}, {#await EXPR then v},
//     {#key EXPR}, {:else if EXPR}, {@const NAME = EXPR}, {@html EXPR}, {@render EXPR})
//     surface as CallSites via MarkupCalls (markup_calls.go), the SOLE producer of
//     the template region: ExtractCalls runs the delegated CallsQuery only over the
//     extracted <script> region (ScriptCalls, scriptCallSource), never the whole
//     .svelte file, so template calls are not ALSO double-emitted — garbled — by
//     tree-sitter error-recovery over the raw markup. The sigil keyword and binding
//     clause are excluded, so only the expression's calls/refs become edges (go-code
//     models no control-flow-structure edges: this is effective control-flow parity).
//     Astro shares this same two-region split; its identical pre-existing double-emit
//     on non-JSX exprs (e.g. {user.greet()}) is closed by the same seam.
//   - NOT classified as runes: $$slots/$$props/$$restProps (Svelte 4 legacy),
//     $.proxy/$.computed/etc. (Svelte 5 internals), $inspect.with (chained method).
//
// Not supported (silently ignored, matches plan scope):
//   - The native tree-sitter-svelte grammar (block STRUCTURE edges), <slot>, and
//     <style> blocks. Control-flow / special tags contribute their header EXPR's
//     calls/refs (see above), not structure edges.
//   - Symbol (function/type/const) extraction from the template body — only the
//     <script> blocks contribute symbols.
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

// Parse extracts <script> blocks and parses them ONCE with the TypeScript grammar
// to yield both the ordinary symbols and the Svelte 5 rune symbols from a single
// tree (parseSvelteWithRunes), remaps every symbol line number back to the
// original .svelte file coordinates, and populates TemplateRefs from capitalised
// component tags in the markup.
func (h *svelteHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	vs, refs := preproc.ExtractSvelteWithRefs(src)
	result, err := parseSvelteWithRunes(path, vs, "svelte", opts)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		result.TemplateRefs = refs
	}
	return result, nil
}
