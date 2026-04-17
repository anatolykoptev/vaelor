# Svelte 5 Runes — Implementation Patterns Research

**Date**: 2026-04-16
**Status**: Final — ready for implementation

## 1. Canonical Rune List

Source of truth: `sveltejs/svelte` `packages/svelte/src/compiler/phases/2-analyze/visitors/CallExpression.js`
(actual switch cases in the compiler's analysis phase, as of 2026-04):

| Rune (exact string) | Category | Notes |
|---|---|---|
| `$state` | state | reactive variable |
| `$state.raw` | state | non-proxied; for large datasets |
| `$derived` | derived | memoized expression |
| `$derived.by` | derived | thunk form for complex logic |
| `$effect` | effect | runs after DOM update |
| `$effect.pre` | effect | runs before DOM update |
| `$effect.root` | effect | manual lifecycle — returns cleanup fn |
| `$effect.tracking` | effect | returns bool: are we inside a reactive context? |
| `$effect.pending` | effect | dev mode async pending state |
| `$props` | props | destructured component props |
| `$props.id` | props | stable prop ID (Svelte 5.x addition) |
| `$bindable` | props | marks prop as two-way bindable; used inside `$props()` default |
| `$inspect` | inspect | dev-mode reactive logging |
| `$inspect.trace` | inspect | function-level trace (first statement in fn only) |
| `$host` | host | access host element in custom elements only |

Note: `$state.snapshot` and `$state.eager` appear in compiler source but as utility/internal helpers, not as binding-creating case labels. Omit from detector v1.

**eslint-plugin-svelte** (`src/shared/runes.ts`) maintains a shorter 7-item root-name set:
`$state`, `$derived`, `$effect`, `$props`, `$bindable`, `$inspect`, `$host`.

## 2. Detection Patterns from Upstream Tools

**svelte/compiler** (`CallExpression.js`): Uses `get_rune(node, scope)` that walks the callee AST. For `$state(0)` the callee is `Identifier{name:"$state"}`. For `$state.raw([])` the callee is `MemberExpression{object:Identifier{name:"$state"}, property:Identifier{name:"raw"}}`. The function checks `scope.get(name)` returns `null` (name is NOT shadowed by a user variable) before classifying as a rune. `VariableDeclarator.js` calls `get_rune(init, scope)` and assigns `kind = 'state' | 'raw_state' | 'derived' | 'prop'`.

**svelte-language-tools** (`svelte2tsx`): No dedicated rune detector; detection spread through `processInstanceScriptContent.ts` using text comparison (`getText() === '$props()'`). Not reusable — breaks on generics/whitespace.

**eslint-plugin-svelte**: Detects runes as `node.init?.type === 'CallExpression' && node.init.callee?.type === 'Identifier' && node.init.callee?.name === '$props'` on ESTree `VariableDeclarator` nodes. For dotted variants: checks `SVELTE_RUNES.has(node.object.name)` (root-name set only).

## 3. Decisions for Our Implementation

**Destructuring of `$props()`** — emit ONE `KindRune / RuneKind="props"` symbol at the `$props()` call location. The compiler assigns `kind='prop'` or `kind='bindable_prop'` per destructured identifier, but for go-code purposes the call-site symbol is sufficient. Destructured names remain `KindVariable`.

**`.svelte.ts` / `.svelte.js` support** — implement now, trivially: check `strings.HasSuffix(filename, ".svelte.ts") || strings.HasSuffix(filename, ".svelte.js")` and run the same rune post-pass (no script-block extraction needed — these files are pure TypeScript).

**Dotted-variant matching** — use per-variant string entries in a map. Match: (a) callee is `Identifier` → name must be in rune set; (b) callee is `MemberExpression` → concatenate `object.name + "." + property.name` and look up in the full variant map. `RuneKind` = root category (`"state"`, `"derived"`, etc.); symbol name encodes the variant (`"$state.raw"`).

**Scope-shadowing check** — v2 concern. In practice, no one shadows `$state` with a user variable.

## 4. Test Fixtures

Best sources (all in `sveltejs/svelte`):
- `packages/svelte/tests/runtime-runes/samples/` — hundreds of `.svelte` files covering every rune variant
- `packages/svelte/tests/validator/samples/` — files starting `rune-` / `runes-` cover invalid placements

Minimal fixture set to write in-repo:
1. `let count = $state(0);` + `let doubled = $derived(count * 2);`
2. `let items = $state.raw([1, 2, 3]);` + `let sorted = $derived.by(() => [...items].sort());`
3. `$effect(() => console.log(count));` (no LHS — effect must be detected even without variable binding)
4. `let { name = "anon" } = $props();` with `let { x = $bindable(0) } = $props();` in same file

## 5. Anti-Patterns

- **Matching `$` prefix broadly** — `$store` (Svelte store subscription) starts with `$` but is NOT a rune. Rune names are in a fixed closed set AND the callee is a `call_expression`. Store subscriptions appear as plain `Identifier` references, not callees.
- **Svelte 4 legacy identifiers** — `$props`, `$restProps`, `$slots` existed in Svelte 4 as compiler-injected identifiers (not calls). A bare `$props` reference (not followed by `()`) is NOT a rune call. eslint-plugin-svelte uses `/^\$[^\$]/` to identify single-dollar names but then checks for `CallExpression` to distinguish runes from legacy references.
- **Text-matching on source** — `getText() === '$props()'` breaks on whitespace, TypeScript generics (`$state<number>(0)`), or comments. Use AST node type + name, not raw text.
- **MemberExpression property lookup** — for a `$state.raw` callee, `property.name` is `"raw"`. Don't look up `"raw"` alone; concatenate to `"$state.raw"` first.
- **`$inspect().with` chaining** — the outer `$inspect(...)` call is the rune; `.with(handler)` is a method on the result. For v1, classify outer call as `RuneKind="inspect"`, ignore `.with`.
- **Rune outside valid file extensions** — guard the detector: only run on `.svelte`, `.svelte.ts`, `.svelte.js`.

## Sources

- `sveltejs/svelte` `packages/svelte/src/compiler/phases/2-analyze/visitors/CallExpression.js`
- `sveltejs/svelte` `packages/svelte/src/compiler/phases/2-analyze/visitors/VariableDeclarator.js`
- `sveltejs/eslint-plugin-svelte` `packages/eslint-plugin-svelte/src/shared/runes.ts`
- `sveltejs/eslint-plugin-svelte` `packages/eslint-plugin-svelte/src/rules/valid-prop-names-in-kit-pages.ts`
- `sveltejs/language-tools` `packages/svelte2tsx/src/svelte2tsx/processInstanceScriptContent.ts` — text-match (don't copy)
