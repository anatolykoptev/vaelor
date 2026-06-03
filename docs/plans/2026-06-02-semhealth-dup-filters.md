---
title: semhealth duplicate filter chain (find-duplicates, option-3 reuse path)
date: 2026-06-02
base: origin/main @ de3a099
supersedes: deploy/deploy-config/plans/go-code/2026-06-02-find-duplicates-semantic-clone.md (option 3)
risk: M
phases: 4
---

# Revised execution plan — `find_duplicates` via extending `semhealth`

## Why this differs from the architect plan

The architect plan (`…/find-duplicates-semantic-clone.md`) opens with **"There is no
intra-repo same-thing-by-meaning detector"** and proposes a greenfield `find_duplicates`
MCP tool over a new `Store.FindNearDuplicates` self-join. **Ground-truth audit at
`de3a099` contradicts the premise:**

- `Store.FindSimilarPairs` (`internal/embeddings/store_similarity.go`) — pgvector
  all-pairs cosine self-join, repo-scoped, `statement_timeout=15s` guard, unique-pair
  trick `(file:sym) < (file:sym)`. **Already the candidate generator** (plan Phase 1).
- `internal/semhealth/` — `Analyze`, `ComputeSemanticDupRatio`, `CollectDupGroups`
  (union-find clustering), `DupGroup`/`DupSymbol`, repo-size guard
  `semhealthMaxFuncs=5000`. **Already the grouping + max-symbols guard** (plan Phase 1/3).
- Surfaced via `code_health` tool, `focus=semantic_duplicates`
  (`buildHealthSnapshot` → `collectSemanticDupGroups` → `buildSemanticDupXML`).
  **A triage surface already exists** — just unfiltered.

**Operator decision (2026-06-02): option 3 — extend `semhealth`, no new MCP tool.**
The value the plan correctly identifies ("Phase 2 filters ARE the product; Phase 1/3 are
plumbing") is genuinely net-new: `semhealth` does **zero** false-positive filtering, so
its `DupGroups` are polluted by interface impls, test mirrors, caller/callee pairs. We add
the filter chain + exact tier + tiering into `semhealth`, and enrich the existing
`focus=semantic_duplicates` output. No `tool_find_duplicates.go`, no `register.go` change.

## Audit-verified reuse points (file:line @ de3a099)

| Need | Reuse | Note |
|------|-------|------|
| Candidate generation | `Store.FindSimilarPairs` store_similarity.go:70 | all-pairs self-join, 15s timeout guard, 57014→empty |
| Pair type | `SimilarPair` store_similarity.go:30 | add `KindA,KindB` (currently absent) |
| Grouping | `CollectDupGroups` semhealth.go:64 | union-find; keep as-is, feed it filtered pairs |
| Repo-size guard | `semhealthMaxFuncs=5000` semhealth.go:26 | the plan's max_symbols guard — already exists |
| Graph access path | `Expander.execCypherN` + `buildNameFilter` expand.go:108,162 | unexported; same package → add exported `Expander` method |
| Graph edges present | CALLS **and IMPLEMENTS** (AGE probe 2026-06-02) | filter 6 graduates from heuristic → exact query |
| Test-file detection | `langutil.IsTestFile` testfile.go:60 | reuse — do not reinvent |
| body_hash column | store.go:65 `body_hash BIGINT` | exact-tier, zero vector |
| Graph name | `codegraph.GraphNameFor(root)` store_helpers.go:31 | repo_key = graph name |
| Deps already carry Expander | `SemanticDeps.Expander` tool_semantic_search.go:36 | wiring ~zero; spy pattern at :190/:278 |
| Metrics pattern | `sparseBackfill*` backfill.go:36 | promauto + init() pre-touch, port 9897 |
| XML output | `buildSemanticDupXML` tool_code_health_reports.go:121 | extend additively (tier, kind, exact, filter reasons) |
| Test double pattern | `countingFinder` semhealth_test.go:150 | mirror for filter unit tests |

## AGE edge inventory (probe 2026-06-02, every `code_*` graph)
`BELONGS_TO, CALLS, CONTAINS, FETCHES, HANDLES, IMPLEMENTS, IMPORTS, INHERITS, TESTED_BY, USES`
→ **CALLS + IMPLEMENTS both present.** Filter 6 (interface-sibling suppression) is an exact
graph query, not the approximate "same name across ≥3 files" fallback.

---

## Phase 1 — extend the engine (`internal/embeddings`)
**Files:** `store_similarity.go`, `store_dup.go` (new, exact tier), tests.
1. Add `KindA, KindB string` to `SimilarPair`; SELECT `a.symbol_kind, b.symbol_kind` in
   `FindSimilarPairs`, extend the `rows.Scan` at store_similarity.go:120. **Additive** —
   existing consumers (`ComputeSemanticDupRatio`, `CollectDupGroups`) read named fields,
   unaffected.
2. New `store_dup.go`: `Store.FindExactDuplicates(ctx, repoKey) ([]ExactDupPair, error)` —
   repo-scoped `GROUP BY body_hash HAVING COUNT(*)>1` across distinct `(file_path,
   symbol_name)`, `body_hash <> 0`. Zero vector cost. `ExactDupPair{A,B SymbolRef}`.
3. Two-tier stays caller-side: `FindSimilarPairs` returns all pairs `< T2`; tier assigned
   in semhealth from distance. Named consts `dupTierVeryCloseDist`, `dupTierRelatedDist`.
4. Unit tests: kind columns scanned; exact-tier self-excludes same `(file,sym)`,
   repo-scoped, ignores `body_hash=0`.
- **Risk M** — touches a struct consumed by shipped `code_health`; additive only, keep green.

## Phase 2 — filter chain (`internal/semhealth`) — THE PRODUCT
**Files:** `dupfilter.go` (new), `Expander` method in embeddings, tests.
1. `embeddings`: exported `func (e *Expander) PairsConnectedByCalls(ctx, graphName string,
   pairs []PairKey) (map[PairKey]bool, error)` and `PairsSharingInterface(...)` — batch
   one Cypher each via `execCypherN`+`buildNameFilter` (fwd+rev for CALLS;
   `(a)-[:IMPLEMENTS]->(i)<-[:IMPLEMENTS]-(b)` for siblings). Graph-missing → nil map, nil
   err (graceful, same as `Expand`). `PairKey` = canonical `(fileA:symA, fileB:symB)`.
2. `semhealth/dupfilter.go`: pure `[]SimilarPair → []SimilarPair`:
   - `filterSameFile` (default drop same `file_path`; `IncludeSameFile` escape).
   - `filterTests` (drop if either side `langutil.IsTestFile`).
   - `filterKind` (drop low-signal kinds `field/var/const/import`; keep
     `function`/`method`, keep `function↔method` cross).
   - graph filters via injected interface (test seam): `filterCallsEdges`,
     `filterInterfaceSiblings`.
   Each filter returns dropped-count for metrics. Composable chain.
3. Unit tests with labeled negatives as fixtures (interface `Search` quartet, test mirror,
   caller/callee pair, getter/setter).
- **Risk M** — correctness of the filter set is the whole value; review effort concentrates here.

## Phase 3 — surface in `code_health` + metrics
**Files:** `semhealth.go`, `tool_code_health_stages.go`, `tool_code_health_reports.go`,
`dup_metrics.go` (new).
1. New `semhealth.AnalyzeTriage(ctx, deps, repoKey, totalFuncs, opts) *TriageResult` —
   runs `FindSimilarPairs` + `FindExactDuplicates`, applies filter chain, assigns tiers
   (`exact` / `very-close` / `related`), groups via `CollectDupGroups`. `deps` carries the
   graph-filter interface (satisfied by `*embeddings.Expander`).
   **Keep `Analyze` (ratio path) UNCHANGED** — do NOT move `code_health` grades until the
   Phase-4 gate validates the filters. `gatherHealthSemanticDup` stays on `Analyze`.
2. `collectSemanticDupGroups` → call `AnalyzeTriage` (filtered) for the
   `focus=semantic_duplicates` report only. Thread `semDeps.Expander` (already present).
3. `DupGroup` gains `Tier string`; `DupSymbol` gains `Kind string`; extend
   `buildSemanticDupXML` (tier attr, kind attr, exact-tier section, `passed=` filter
   reasons). Additive — no break when triage not requested.
4. Metrics (`promauto`, port 9897, `init()` pre-touch):
   `gocode_semhealth_dup_candidates_total{repo}` (pre-filter),
   `gocode_semhealth_dup_reported_total{tier}` (post-filter),
   `gocode_semhealth_dup_filtered_total{filter}` (per-filter drop — which filter works),
   `gocode_semhealth_dup_duration_seconds`. DB/AGE errors bump a metric or log (never silent).
- **Risk S** — plumbing; the cold-path guarantee (byte-identical when triage not requested) is the watch-item.

## Phase 4 — validation gate (decides ship/no-ship)
**Files:** `internal/semhealth/dup_labeled_test.go` (integration, build-tagged), `docs/find-duplicates.md`.
- Labeled positives (real dups) + negatives (legit similars that MUST be filtered) per the
  architect plan §3.4. Run full pipeline against go-code's own live index.
- Assert: every positive surfaces; precision@10 ≥ **0.6**; every labeled negative absent;
  interface-impl leakage specifically measured (the §6 risk).
- Needs `DATABASE_URL` + indexed go-code → integration test, build-tagged, runs on host-a
  local runner (GHA deleted). `GOWORK=off`, CGO, vendored.
- `docs/find-duplicates.md` records calibrated thresholds + precision result + the explicit
  ship / flag / shelve decision.
- **Risk S** (test-only) but **this phase decides the recommendation.**

## Constraints
- Builds ON existing self-join + semhealth + AGE; net-new = kind cols + exact tier + filter
  chain + 2 Expander methods + triage path + tiered XML + metrics + labeled test.
- gofmt-clean, gocognit < 20 (filters single-loop/map-lookup), no magic numbers (tiers/kinds
  as named const), write-failures logged/counted.
- `Analyze` ratio path untouched (no silent grade shift); `code_health` byte-identical when
  `focus != semantic_duplicates`.
- `go-code` MCP `review_delta` (base=main) before PR.
