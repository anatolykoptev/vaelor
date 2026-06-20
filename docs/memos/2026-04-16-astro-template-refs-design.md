# Astro Template Component References — Design Memo

**Date**: 2026-04-16
**Author**: post-v1.17.0 follow-up, Task 13
**Status**: Implemented — Option A + USES edge + §4.7 in-memory UsesIndex wiring (commits 2a15fda, b5bbd48, 2026-04-17).

## 1. Problem

`internal/parser/preproc/astro.go::ExtractAstro` produces a `VirtualSource` from
the frontmatter (`---`-fenced) and `<script>` tags only. Everything between
those — the Astro template body — is discarded before tree-sitter sees it.

Consequence: capitalized JSX-style usages such as `<Breadcrumbs />`, `<Header />`,
`<Footer client:load />` are invisible to the symbol/edge pipeline. Fan-in
metrics (callers in `understand`, edges in `code_graph`/`dep_graph`,
blast-radius in `impact_analysis`) under-report Astro components — typically
to zero callers, even when the component is rendered on every page.

Concrete example (example.org Astro frontend):

```astro
---
import Breadcrumbs from "../components/Breadcrumbs.astro";
import Header from "../layouts/Header.astro";
const items = [...];
---
<Header />
<main>
  <Breadcrumbs items={items} />
  <slot />
</main>
```

After parsing today: `Breadcrumbs` and `Header` appear ONLY as bound import
identifiers in the frontmatter TS buffer. The TS grammar correctly records
the imports (`ParseResult.Imports`), but never records that they are *used*.
`call_trace --reverse Breadcrumbs` returns 0 callers. `dep_graph` shows the
component as a leaf island. This biases `code_health`, `dead_code`
("unused exported function"), and any LLM that consumes graph context.

The same issue applies to Svelte (`<MyComponent />` in template body) but
this memo scopes to Astro; the design generalises trivially.

## 2. Design space

### Option A — Extend `ExtractAstro` to emit ref records alongside the virtual source

Change `ExtractAstro` signature:

```go
type AstroExtract struct {
    Virtual   *VirtualSource
    TplRefs   []TemplateRef // capitalized tags found in template body
}

type TemplateRef struct {
    Name string  // "Breadcrumbs"
    Line uint32  // 1-based, in original .astro coords
    Col  uint32
}
```

Caller (`handler_astro.go`) attaches refs to `ParseResult` via a new field.

- Pros: single-pass (we already walk the file); no public API churn for non-preproc langs; natural place — preproc already understands template/non-template regions.
- Cons: changes a stable signature; needs new `ParseResult.TemplateRefs` field touched by every consumer that snapshots the result.

### Option B — Standalone scanner `preproc.ScanTemplateRefs(src) []TemplateRef`

Run as a sibling pass invoked from `handler_astro.go::Parse` after the
virtual-source build. Operates on the regions that `ExtractAstro` already
classified as template body (i.e. the inverse of frontmatter+scripts).

- Pros: zero coupling to `ExtractAstro`; easy to disable behind a flag; testable in isolation.
- Cons: needs to re-derive template regions OR have `ExtractAstro` expose them — duplication risk.

### Option C — Synthesise fake call expressions inside the virtual source

For each `<Foo ... />`, inject `Foo();` (or `void Foo;`) into the TS buffer
on a padding line. The existing TS call-graph extraction then picks them up
as references with no downstream changes.

- Pros: zero changes to ParseResult / snapshot / codegraph / tools — completely transparent. Reuses existing call-edge pipeline.
- Cons: line-mapping must be precise (refs map back to template line, not synthetic line); `void Foo;` reads as a use only for the `references`/identifier pass — to also count as a CALLS edge it must be a call expression; muddies "call" semantics (a render is not a function call); harder to label the edge as USES vs CALLS later.

### Option D — Defer entirely; add a caller-driven scan in `analyze` / `callgraph`

When fan-in is queried, the analyzer walks Astro files in the repo and
greps for capitalized tags on demand.

- Pros: zero parser-level change; pay only on query.
- Cons: every fan-in tool would need to know about it; cache invalidation is bespoke; doesn't show up in snapshots/diff/dep_graph; defeats the point of an AST pipeline.

## 3. Recommended design — **Option A + edge label "USES"**

Recommended because:

1. The single-pass cost is negligible: the template body is already in
   memory, the regex/scanner is trivial (see §6), and Astro files are
   typically <500 lines.
2. Keeps semantics clean — a template render is *not* a function call;
   modelling it as a separate edge label `USES` (parallel to `CALLS`,
   `IMPORTS`, `IMPLEMENTS`, `INHERITS`) is honest and lets weighting and
   tools treat it differently if needed.
3. Snapshots/dep_graph/impact_analysis already iterate `semanticEdgeLabels`
   — adding `"USES"` to the map is one line. Existing `code_graph` Cypher
   templates can stay; new ones can opt-in by relationship label.
4. Resolution piggybacks on `ParseResult.Imports`: `<Breadcrumbs />` →
   look up `Breadcrumbs` in import bindings → resolved file path. The
   parser already has this data; `analyze` just joins them.

Reject **C** (semantic muddle), **D** (no graph integration), **B**
(needless duplication of region tracking).

## 4. Implementation outline

### 4.1 Files to touch

| File | Change |
|------|--------|
| `internal/parser/preproc/preproc.go` | Add `TemplateRef` type (`Name`, `Line`, `Col`). |
| `internal/parser/preproc/astro.go` | New `ExtractAstroWithRefs(src) (*VirtualSource, []TemplateRef)`. Existing `ExtractAstro` becomes a thin wrapper that drops refs (preserves API). |
| `internal/parser/parser.go` | Add `ParseResult.TemplateRefs []TemplateRef` (JSON `template_refs,omitempty`). |
| `internal/parser/handler_astro.go` | Call new extractor; populate `ParseResult.TemplateRefs`. |
| `internal/parser/handler_svelte.go` | Mirror for Svelte (follow-up; not required for v1). |
| `internal/callgraph/` | New helper `templateRefEdges(result *ParseResult) []Edge` that joins TemplateRefs against `Imports` to produce `USES` edges. |
| `internal/codegraph/community.go` | Add `"USES": 1` to `edgeWeights` (low weight — it's a render dep, not a call). |
| `internal/codegraph/snapshot.go` | Add `"USES"` to `semanticEdgeLabels`. |
| `internal/codegraph/graph_build.go` | Emit `USES` edges from template refs (mirroring `IMPORTS` block). |
| `internal/parser/queries/` | No new tree-sitter query — scan is regex/state-machine in preproc. |
| `internal/parser/preproc/astro_test.go` | New tests: capitalised tag detection, namespaced skip, conditional, slot, self-closing, dynamic component (`<Comp:is>`), HTML entities, comment-stripped regions. |

### 4.2 Scanner sketch

In `preproc.scanTemplateRefs(region []byte, baseLine uint32) []TemplateRef`:

State machine (no full HTML parse):

1. Walk byte by byte. Skip `<!-- ... -->` comments. Skip `<style>...</style>` and `<script>...</script>` ranges (already known).
2. On `<` followed by `[A-Z]`: read the tag name (`[A-Za-z0-9_$.]+`).
3. Skip if name contains `:` or `-` AND prefix is a known namespace (`astro:`, `svelte:`, `client:`). Hyphenated capitalised customs (`<MyEl-foo />`) are still recorded.
4. Record `TemplateRef{Name, Line, Col}` once per occurrence (NOT deduped — a component used 5 times has 5 refs; downstream can dedupe per query).
5. Advance past the tag (find matching `>` honouring quoted attribute values — same trick already used in script-tag scanner with `tagOpenScanLimit`).

Closing tags (`</Foo>`) are ignored — opening recorded the use.

Dynamic components (`<Component:is={Foo}>`) — record the *string* `Foo`
extracted from the brace; if not a bare identifier, skip (mark unresolved).
v1 may simply skip these and log a `parser_unresolved_dynamic_component`
metric.

### 4.3 Resolution (in callgraph)

```go
func resolveTemplateRef(ref TemplateRef, result *ParseResult, repoIdx *RepoIndex) (string, bool) {
    importPath, ok := lookupBinding(result.Imports, ref.Name)  // "../components/Breadcrumbs.astro"
    if !ok { return "", false }                                // unresolved (global / ambient)
    return repoIdx.Resolve(result.File, importPath), true
}
```

Emit edge `USES (file, ref.Line) → (resolved file, symbol="default")`. For
`.astro`/`.svelte`/`.vue` defaults, the imported file *is* the symbol; no
inner symbol disambiguation needed.

### 4.4 Edge cases handled

| Case | Behaviour |
|------|-----------|
| Lowercase HTML (`<div>`, `<header>`) | Skipped. |
| Namespaced (`<svelte:head>`, `<astro:fragment>`, `<astro:slot>`) | Skipped. |
| `client:*` directive (`<Foo client:load />`) | Recorded as `Foo` once; directive ignored. |
| Conditional (`{cond && <Foo />}`) | Recorded — brace context doesn't suppress. |
| Slots (`<Foo><span slot="x" /></Foo>`) | `Foo` recorded once on opening tag. Inner lowercase ignored. |
| Self-closing (`<Foo />`) | Recorded. |
| Nested (`<Foo><Bar /></Foo>`) | Both recorded. |
| Spread / dynamic name (`<Component:is={Foo}>`) | Best-effort: bare ident inside braces recorded; complex expr → unresolved metric. |
| Inside `<style>` / `<script>` | Skipped (regions already known). |
| Inside `<!-- comments -->` | Skipped. |
| Multiple uses of same component | Each recorded; downstream dedupe per query. |
| Component imported but never used | No `USES` edge — correct. `dead_code` may now flag it. |
| Used but not imported (global / `Astro.props.X`) | Recorded as ref; resolution returns unresolved; tools should accept unresolved refs and surface as a degradation note. |

### 4.5 Performance

Astro files in example.org: 50–500 LOC, p99 ~2 KB. The scanner is one byte
walk with one branch per `<` — comparable cost to the existing script-tag
locator (already in `ExtractAstro`). Estimate: <0.5 ms per file on warm
cache, dominated by the existing tree-sitter parse (~5–20 ms). Total
overhead <5%.

A regex (`<[A-Z][A-Za-z0-9_]*\b`) would also work but doesn't handle
comment/script stripping cleanly. Prefer the state machine.

### 4.6 Integration with existing systems

- `dep_graph` / `code_graph`: AGE upserts a new `USES` relationship type. Cypher templates stay; new ones can `MATCH ()-[:USES]->()` if desired.
- `impact_analysis`: **USES edges are wired** via `internal/impact/uses.go:appendUsesCallers`, called from `impact.go:81` as a file-level fall-through when a symbol lookup finds no in-memory CALLS callers. The `callgraph.CallGraph.UsesIndex` (map from target-file → []using-file) is populated by `buildUsesIndex` at `callgraph/repo.go:123`. This is the "v2 option 1 in-memory wiring" described in §4.7 — it shipped in the same commit batch (2026-04-17).
- `understand`: USES-based callers are surfaced through the same `UsesIndex` path; Astro component callers appear in `impact_analysis` and `understand` results as file-level USES callers.
- `dead_code`: now correctly excludes Astro components that are referenced from templates.
- Snapshots / diff: `USES` added/removed appears in `review_delta` output naturally once added to `semanticEdgeLabels`.

### 4.7 impact_analysis and understand integration — SHIPPED (2026-04-17)

**Implemented as v2 option 1 (in-memory wiring).** `callgraph.CallGraph` now carries a
`UsesIndex map[string][]string` (target-file → []using-file), populated by
`buildUsesIndex` at `callgraph/repo.go:123` which calls `ResolveTemplateRefs`
(`internal/callgraph/astro_resolve.go:31`) against every `.astro` parse result.

`internal/impact/uses.go:appendUsesCallers` (called from `impact.go:81`) performs
the file-level fall-through lookup: when `findTarget` returns nil for an Astro
component path, `appendUsesCallers` queries `cg.UsesIndex` and appends callers to
the result. This means `impact_analysis(Breadcrumbs.astro)` correctly returns the
files that render the component.

The "v2 option 2" (AGE-based caller lookup) was not needed and was not implemented.

## 5. Open questions

1. **USES vs CALLS edge label** — RESOLVED (2026-04-17): shipped as a separate file-level `appendUsesCallers` path in `impact_analysis`, not merged into the CALLS callers count. Callers from template refs surface as a distinct entry via the UsesIndex fall-through (§4.7).
2. **Edge weight** — `"USES": 1` proposed (lower than `CALLS: 3`). Reasonable for community detection? Empirical tuning needed once we re-cluster a real Astro repo.
3. **Resolution for re-exports** — `import Breadcrumbs from '../ui'` where `ui/index.ts` re-exports `Breadcrumbs.astro`. Out of scope for v1; mark unresolved, log metric.
4. **Svelte parity** — same scanner works for `<MyComponent />` in Svelte template body. Land as v2 in same PR or split? Recommend split — Svelte preproc has `<script module>` quirks worth their own pass.
5. **Tree-sitter Astro grammar** — there is an official `tree-sitter-astro` (community-maintained). Adopting it would replace the entire preproc pipeline and give us template AST for free. Worth a separate spike; this memo assumes we stay on the preproc-stripping approach for the foreseeable future.
6. **Vue SFC** — same problem class; not addressed. Adding a Vue handler would benefit from this scanner being package-public (`preproc.scanTemplateRefs`).
7. **`Astro.props.X` dynamic component** — `<X />` where `X` is destructured from props. No static resolution possible. Should we record an unresolved ref for completeness, or skip silently? Recommend: record with `Resolved=false`, surface in a degradation metric like `internal/tier`.

## 6. Decision

Adopted **Option A** with new edge label **`USES`**. Shipped 2026-04-17 (commits 2a15fda, b5bbd48):
- (a) §5.1 callers bucket: resolved as separate file-level path in `impact_analysis` (§4.7).
- (b) §5.4 Svelte split: deferred — Svelte parity not yet implemented (`handler_svelte.go` not wired).
