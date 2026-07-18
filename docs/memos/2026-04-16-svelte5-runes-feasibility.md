# Svelte 5 Runes as First-Class Symbols — Feasibility Memo

**Date**: 2026-04-16
**Author**: post-v1.17.0 follow-up, Task 12
**Status**: Research only. Recommendation: **defer**.

## 1. Problem

`internal/parser/handler_svelte.go` parses `.svelte` files by pulling every
`<script>` / `<script context="module">` / `<script module>` block out of the
component (`preproc/svelte.go::ExtractSvelte`) and handing the concatenated
text to the TypeScript tree-sitter grammar via `parseWithTSAndRemap`. Offsets
are remapped back to the original file so symbol line numbers stay correct.

Consequence for Svelte 5: the runes API — `$state(…)`, `$derived(…)`,
`$derived.by(…)`, `$effect(…)`, `$props()`, `$bindable()`, `$inspect()` — is
valid JavaScript/TypeScript. The TS grammar sees them as plain call
expressions. So in

```svelte
<script>
  let count = $state(0);
  let doubled = $derived(count * 2);
  $effect(() => console.log(count));
</script>
```

`count` and `doubled` land as generic `KindVariable` symbols (if the TS query
captures them — currently it only captures arrow-function `const`s, per the
comment in `handler_svelte_test.go:54`). The *runes semantics* — "this binding
is reactive state", "this is a computed dependency", "this is a side-effect
subscriber" — is lost. There is no way, today, to write a `code_graph` query
like "which components mutate `$state` bindings" or to mark reactive
dependencies as edges in `dep_graph`.

**How badly does this hurt?** Nil, so far. We have zero GitHub issues against
runes handling, and the existing Svelte symbol tests pass (they assert only
that top-level functions and `helper` vars are discovered). The degradation is
invisible until someone asks for Svelte-5-specific analytics.

## 2. Options surveyed

### 2.1 `tree-sitter-grammars/tree-sitter-svelte` (community-maintained fork)

- **Maintainer**: `tree-sitter-grammars` GitHub org (umbrella for adopted
  grammars; authored originally by Amaan Qureshi).
- **Last commit**: 2024-10-19 (`feat(queries): inject stylus when detected`).
  ~18 months stale as of 2026-04.
- **Shape**: grammar extends `tree-sitter-html`. Template features are
  first-class: `if_statement`, `each_statement`, `await_statement`,
  `snippet_statement`, `render_tag`, `html_tag`, `const_tag`, `debug_tag`.
- **Script handling**: `<script>` content is emitted as a single `raw_text`
  token (see `externals: [$.raw_text …]` in `grammar.js`). The grammar relies
  on editor-side **language injection** to re-parse that blob with the
  JavaScript/TypeScript grammar. Test corpus has `main.txt`, `balancing.txt`,
  `snippets.txt`, `html/` — **no `svelte5.txt` / runes fixtures**.
- **Runes support**: none at grammar level. Runes live inside `raw_text`. Even
  after injection, a JS/TS parser sees them as call expressions — same outcome
  as our current pipeline.

### 2.2 `Himujjal/tree-sitter-svelte` (original)

- Last commit 2024-09-07 (merging a newline-in-expressions fix). Declared
  "unmaintained for ~2 years" in `sveltejs/language-tools#2997`. Same
  architectural shape as 2.1 (scripts → `raw_text` → inject). Same runes
  blind spot.

### 2.3 `smacker/go-tree-sitter/svelte` (Go binding)

- Exists. `binding.go`:
  ```go
  package svelte
  //#include "parser.h"
  //TSLanguage *tree_sitter_svelte();
  import "C"
  func GetLanguage() *sitter.Language { … }
  ```
- Vendored grammar is based on the **Himujjal** lineage (confirmed by other
  downstream Go consumers: `zachsnow/redunce`, `1broseidon/cymbal`). Nothing
  structurally different from 2.1 for runes purposes.

### 2.4 Svelte official AST (`svelte/compiler` `parse`)

- Produces a true Svelte-5-aware AST with `RuneCallExpression`-style nodes.
- JavaScript-only (Node runtime). No Go binding, no CGO wrapper. Embedding
  would require either (a) a Node sidecar process per parse, or (b) compiling
  the compiler to Wasm and calling it from Go. Both blow the complexity
  budget for one language and lose tree-sitter's incremental / query story.

### 2.5 Custom grammar (write our own runes layer)

- Approach: keep the TS grammar as base, add a post-parse pass that walks
  `call_expression` nodes whose `function` text matches `^\$(state|derived|
  effect|props|bindable|inspect)(\.(by|raw|snapshot))?$` and synthesizes a
  dedicated `Rune` symbol kind + edge type.
- No new parser dependency. All logic lives in `internal/parser/handler_svelte.go`
  and the shared symbol map.

## 3. Tradeoffs

| Axis | swap to tree-sitter-svelte | post-parse detection (2.5) | status quo |
|---|---|---|---|
| Runes as distinct symbols | No — same `raw_text` + JS injection result | Yes | No |
| Binary size (CGO) | +~2-5 MB (new grammar C) | 0 | 0 |
| Go build graph | +1 vendor tree (`smacker/go-tree-sitter/svelte`) | 0 | 0 |
| Template markup support (`{#if}`, `<slot>`, component refs) | Yes — the only real win | No | No |
| Maintenance burden | adopt ~18-month-stale grammar | ~150 LOC in our repo | 0 |
| Migration risk | breaks `parseWithTSAndRemap` assumption that input is pure TS; needs a new handler path that **coexists** with TS-via-preproc, because scripts are still TS inside | Low — additive | — |

Key insight: **`tree-sitter-svelte` does not solve the runes problem.** Its
value-add is *template* parsing (markup blocks, component usages, snippets)
— orthogonal to runes. To get runes as distinct symbols even after adopting
the grammar, we'd still need the post-parse detection from 2.5 on the
injected JS subtree. So 2.1 → 2.5 is not an either/or; 2.5 is the only path
that actually addresses the task.

Adoption of 2.1 would be justified if/when we want Svelte template symbols
(parallel to what Task 13 proposes for Astro), not for runes. Those are two
separate conversations.

## 4. Recommendation

**Defer.** Three reasons:

1. **No grammar swap gives us runes for free.** All candidate grammars hand
   script content off to a JS/TS parser, which sees `$state(0)` as a call
   expression. The work to promote runes to first-class symbols is identical
   whether we keep the current preproc path or adopt `tree-sitter-svelte`:
   it's a post-parse walk over `call_expression` nodes.
2. **Zero demand signal.** Current tests don't cover runes; no user has
   reported the gap; `understand` / `semantic_search` / `code_graph` still
   return meaningful results for Svelte components (they pick up
   functions, exports, imports — the things users actually query).
3. **Stale upstream.** Both svelte grammars are 18-24 months behind. Adopting
   either makes us owner of third-party unmaintained C code, with no payoff
   for the stated task.

### Concrete next step if/when adoption becomes justified

Trigger: the first user-facing issue that reads "Vaelor can't tell me which
Svelte components use `$state`" or similar. At that point, implement option
**2.5** — a detector in `handler_svelte.go` that runs after
`parseWithTSAndRemap` and rewrites matching `call_expression` symbols:

```go
// Pseudo-sketch, ~80-120 LOC with tests.
for _, sym := range result.Symbols {
    if sym.Kind == KindCall && isRuneCall(sym.Name) {
        sym.Kind = KindRune       // new kind
        sym.RuneKind = runeKind(sym.Name) // state|derived|effect|props|…
    }
}
```

Keep preproc-based script extraction. No new vendor, no CGO churn. This also
lets us add targeted `code_graph` edges (`REACTS_TO`, `DERIVES_FROM`) without
touching the parser layer.

Revisit grammar swap **only** when the ask is "parse Svelte template markup"
(components used in `.svelte` files, `{#if}`/`{#each}` blocks, etc.) — at
which point the right design memo is "adopt `tree-sitter-svelte` + define
Svelte-specific queries", not "fix runes".

## Sources

- `internal/parser/handler_svelte.go` (current implementation)
- `internal/parser/preproc/svelte.go` (script extraction)
- `internal/parser/handler_svelte_test.go` (existing coverage)
- `github.com/tree-sitter-grammars/tree-sitter-svelte/blob/master/grammar.js`
  (markup-only grammar, scripts → `raw_text`)
- `github.com/tree-sitter-grammars/tree-sitter-svelte/tree/master/test/corpus`
  (no runes fixtures as of 2024-10)
- `github.com/smacker/go-tree-sitter/blob/master/svelte/binding.go`
  (vendor-ready Go binding exists)
- `github.com/sveltejs/language-tools/issues/2997`
  ("Svelte maintained `tree-sitter` grammar" — community maintenance concern)
- Svelte 5 runes reference (svelte.dev docs; runes are `$state`, `$derived`,
  `$derived.by`, `$effect`, `$props`, `$bindable`, `$inspect`).
