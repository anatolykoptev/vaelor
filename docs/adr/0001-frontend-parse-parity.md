# ADR 0001: Frontend-Framework Parse Parity (React/Svelte/Astro)

- **Status:** Accepted (Phases 0a-3 shipped; Phase 4 deferred/operator-gated)
- **Date:** 2026-07-01
- **Arc:** `plans/go-code/2026-06-30-frontend-parse-parity-react-svelte-astro.md`
  (krolik-canonical plan store; PRs #267-#271, this Phase-5 PR)

## Context

React (`.tsx`/`.jsx`) parses the whole file as one TSX tree, so composition,
calls-in-markup, and bare `{count}` refs fall out of the generic tags/calls
queries for free. Svelte and Astro extract-and-delegate only their `<script>`
region (`preproc.VirtualSource` + `parseWithTSAndRemap`), so template markup
was unparsed — a real capability gap, not a cosmetic one, since the call graph
and USES edges feed 48 other agents and 5 review councils.

## Decisions

### 1. Reparse markup `{expr}` ranges with the TSX grammar (tsxLang), not plain tsLang

`parseWithTSAndRemap` (`internal/parser/preproc_remap.go:99-101`) is a thin
wrapper over `parseVirtualWithRemap` bound to the plain-TypeScript grammar
(`tsLang.parserBase`) — correct for `<script>`/frontmatter contents, but
Astro template expressions legally embed JSX
(`{list.map(i => <Card/>)}`). A plain-TS reparse of an isolated `{expr}`
range would emit ERROR nodes and drop those calls/refs outright. `tsxLang` is
a strict superset that parses Svelte's plain-JS exprs fine too, so one shared
`markupExprReparse` path (built on the same `parseVirtualWithRemap` core,
grammar bound to `tsxLang` instead of `tsLang`) serves both frameworks. The
existing `tsLang` wrapper (`parseWithTSAndRemap`) is untouched — same
signature, same behaviour, byte-identical golden test
(`TestTSLangRemapGolden`).

### 2. No `markup_calls.scm` clone — reuse `tsx_calls.scm` + a minimal `markup_refs.scm`

A draft design modeled a new markup-specific calls query on
`tsx_calls.scm`'s `jsx_expression` capture. Because decision 1 reparses with
the real TSX grammar, `tsx_calls.scm` (`internal/parser/queries/tsx_calls.scm`)
fires against the reparsed `{expr}` tree for free — calls, member-calls, and
argrefs all come from the existing query with zero new query surface. The
only genuinely new capability is React's bare `{count}` parity (captured in
React via `jsx_expression` argref, which cannot fire on an *isolated*
extracted expression with no surrounding JSX context) — met by one minimal
`markup_refs.scm` top-level-bare-identifier pattern. A hand-rolled clone of
`tsx_calls.scm` would have been both redundant and, before decision 1
corrected the grammar choice, structurally inapplicable.

### 3. Single-producer region split via the `scriptCallSource` seam

Before this arc, `ExtractCalls` (`internal/parser/calls.go`) ran a preprocessor
handler's raw `CallsQuery` over the WHOLE (non-TypeScript) file. Tree-sitter's
error-recovery parser would then surface template calls too, but GARBLED
(`<p>{user.greet()}</p>` yielded `greet` with `Receiver="{user"`), duplicating
the clean calls the new markup pass produces. The fix: handlers implementing
`scriptCallSource` (`calls.go:25-45`) get their calls routed through
`ScriptCalls` (clean, line-remapped from the extracted script
`VirtualSource`) INSTEAD OF the raw whole-file `CallsQuery`; the template
region is then served SOLELY by `markupCallSource.MarkupCalls`
(`calls.go:47-56`). Result: exactly one producer per region — script calls
from `ScriptCalls`, template calls from `MarkupCalls`, no overlap, no garbled
error-recovery edge. `astroHandler` and `svelteHandler` both implement the
full pair (`internal/parser/markup_calls.go`); `TestNoDuplicateMarkupEdges`
(`internal/parser/markup_parity_test.go`) and
`TestScriptCallSourceImpliesMarkupCallSource` (Phase 5,
`internal/parser/handler_registry_fitness_test.go`) regression-guard this
split — the latter was a Phase-1 council LOW finding, closed here: a handler
implementing `scriptCallSource` without `markupCallSource` would silently
drop its entire template region (no raw fallback, no markup producer).

### 4. Per-extension `Symbol.Language` labeling agreeing with `DetectLanguageFromPath`

`tsxHandler` serves both `.tsx` and `.jsx` through one shared handler
(`internal/parser/handler_tsx.go`), and its `MapCapture` delegates to
`tsLang.MapCapture`, which hardcodes `Symbol.Language = "typescript"` on
every emitted symbol regardless of extension. `DetectLanguageFromPath`
(`internal/parser/parser_lang.go:70`) already correctly maps `.jsx` ->
`"javascript"` (matching GitHub Linguist) via `extLanguageOverride`. The fix
is NOT to change the shared handler's hardcoded label (that would mislabel
every `.tsx` too — a worse fleet-wide regression) but to derive
`Symbol.Language` from the file's own detected language at `Parse()` time via
`applyDetectedSymbolLanguage` (`internal/parser/parser.go`), opted into by
`tsxHandler.Parse` and `typescriptHandler.Parse` (the JS/TS family shares the
identical defect for `.js`/`.mjs`/`.cjs`, caught by council review on the
same PR). The Phase 0b `TestJSTSFamily_SymbolLanguageAgreesWithDetector`
originally pinned the JS/TS-family invariant; Phase 5's
`TestRegistryWideSymbolLanguageAgreesWithDetector`
(`internal/parser/handler_registry_fitness_test.go`) generalizes it across
EVERY registered handler (ranging the live `registry` map, `handler.go:73`,
never a hand-maintained list) so a future multi-language handler cannot
reintroduce the class silently. `TestJSTSFamily_SymbolLanguageAgreesWithDetector`
was later removed (parity follow-ups cleanup, `docs/FOLLOWUPS.md`) as fully
subsumed by the registry-wide test — `TestRegistryWideSymbolLanguageAgreesWithDetector`
is now the sole guard for this invariant.

### 5. Native tree-sitter-svelte grammar: DEFERRED / operator-gated

A native `.svelte` grammar (binding present via the pinned
`github.com/smacker/go-tree-sitter` dependency, `go.mod:17`,
`dd81d9e9be82`) was considered for Phase 4 as the "complete" fix for complex
nested-brace expression parity. It is explicitly DEFERRED, not adopted,
because:

- go-code parses arbitrary, adversarial third-party repositories through a
  shared MCP server used by 48 agents. A C-level segfault in a roughly
  20-month-stale, single-maintainer grammar is uncatchable by Go's
  `recover()` and would kill parsing for every concurrent caller, not just
  the offending request.
- Byte-scan (the sigil-aware `{expr}`/block-header scanner,
  `internal/parser/preproc/svelte_exprs.go`, reparsed via decision 1's
  `markupExprReparse`) already reaches 6 of 7 capability-matrix rows and
  *effective* control-flow parity: go-code models no control-flow-STRUCTURE
  edges at all (neither for React nor Svelte), so "control-flow parity" is
  really the refs/calls INSIDE a `.map`/`{#each}`/`{#if}` body surfacing as
  ordinary edges — which byte-scan already delivers
  (`TestSvelteBlockHeaderCalls`, `TestSvelteExprDelimitingAccuracy`
  documents a 100%-floor quality metric on a hand-verified corpus, both in
  `internal/parser/markup_parity_test.go` /
  `internal/parser/preproc/svelte_exprs_test.go`).
- The residual gap the grammar would close — robust delimiting of complex,
  deeply-nested-brace expressions the byte-scanner might mis-bound — is a
  minority case, not a fleet consumer blocker today.

The hard prerequisite for ever adopting it: a fuzz go/no-go clean run on an
adversarial `.svelte` corpus, plus a parse-timeout bound on the CGO call, plus
an explicit operator go/no-go. If it ever lands, it REPLACES the byte-scan ref
producer for `.svelte` outright (single-producer strangler cutover, matching
decision 3's discipline) rather than running alongside it.

## Consequences

- The capability-matrix equivalence harness
  (`internal/parser/markup_parity_test.go`) and the expr-delimiting-accuracy
  floor (`internal/parser/preproc/svelte_exprs_test.go`) are the two blocking
  regression gates for this arc (Phase 5) — both already asserted hard
  (`t.Errorf`/`t.Fatalf`, never `t.Logf`-only) since Phase 3, run by `go test
  ./internal/parser/...`, gated by `make preflight`
  (`.github/workflows/preflight.yml`), a required merge check on this public
  repo.
- `ParseResult`/`Symbol` changes across this arc were additive-only
  (`TemplateRefs` already existed; new symbols/edges append like runes) —
  the 48-agent / 5-council fleet blast radius was never a breaking-change
  risk.
- Two deferred consolidations (byte-walker retrofit, shared `parseTree`
  helper) are recorded in `docs/FOLLOWUPS.md` as separate, right-sized
  follow-ups rather than folded into this arc.

## Alternatives considered and rejected

- **Plain-tsLang reparse for markup exprs** — rejected (decision 1): fails on
  Astro's legal JSX-in-expression syntax.
- **New `markup_calls.scm` clone** — rejected (decision 2): redundant with
  `tsx_calls.scm` once the grammar choice is corrected.
- **Flip the shared `tsxHandler`'s hardcoded language field to `"javascript"`**
  — rejected (decision 4): mislabels every `.tsx` symbol, a worse
  fleet-wide regression than the `.jsx` bug it fixes.
- **Adopt the native Svelte grammar now (cost is cheap)** — rejected for this
  arc (decision 5): conflates cheap adoption COST with the security-critical
  RISK of an adversarial-input CGO segfault inside a shared multi-tenant
  process; cost and risk are resolved separately, not by one "still cheap
  enough" call.
