# Graph Ecosystem — Cooperating Signals, Not Competing Stores

> **For Claude:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`. Each task lists its subagent tier (Haiku/Sonnet/Opus). Two-stage review (spec → code quality) on Sonnet/Opus tasks.

## Guiding principle

**Сотрудничество вместо конкуренции.** Today we have two graph representations:

- `internal/callgraph/` — ephemeral, in-memory, Go-type-aware, built per request. Consumed by nine internal packages (`impact`, `deadcode`, `explore`, `research`, `review`, `compound`, `codegraph`, etc.) — it is already the **canonical CALLS extractor**.
- `internal/codegraph/` — persistent, Apache AGE, built **on top of** `callgraph` output (confirmed via import graph). Adds analytics (pagerank, community, surprise), graph-diff, cross-language edges (Route/HANDLES/FETCHES/TESTED_BY), Cypher query surface.

**They do NOT duplicate extraction.** `codegraph` reuses `callgraph.BuildCallGraph`. The real problem is the *consumer* side: tools that build on `callgraph` (compound/impact/review/deadcode) cannot see the analytics `codegraph` computed, even when the AGE graph is fresh. Signals live in isolated silos.

**Goal:** let every tool opportunistically enrich its output with whatever signals the ecosystem has at hand. When AGE is present and fresh → pagerank, community, surprise, cross-language reach. When AGE is absent → graceful zero-value fallback. Never a hard dependency.

**Non-goal:** merging the two packages, moving code around, or changing the extraction pipeline. The architecture is already right — only the wiring between producers and consumers is thin.

---

## Current-state map (verified via `mcp__go-code__explore` + grep)

| Signal | Producer | Consumers today | Consumers that should benefit |
|---|---|---|---|
| CALLS edges (typed + speculative) | `callgraph.BuildCallGraph` | 9 packages (via `callgraph`) | ✓ already shared |
| Call trace BFS/DFS | `callgraph.Trace` | `understand`, `call_trace`, `research` | ✓ already shared |
| Pagerank | `codegraph` (after `IndexRepo`) | only `code_graph` tool | **understand, prepare_change, review, call_trace** |
| Community (Louvain) | `codegraph` | only `code_graph` | **prepare_change, review, impact** |
| Surprise score | `codegraph.SurpriseResult` | only `code_graph` (`find hidden dependencies`) | **review_pr, review_pr_post** |
| Route/HANDLES (HTTP handler ↔ route) | `codegraph` via `internal/routes/` | only `code_graph` Cypher | **call_trace, impact_analysis** |
| FETCHES (frontend → backend) | `codegraph` | only `code_graph` | **call_trace (cross-language), review_pr** |
| TESTED_BY | `codegraph.ExtractTestedByEdges` | only `code_graph` | **review_pr, prepare_change, understand** |
| Graph diff (Snapshot before/after) | `codegraph.DiffGraphs` | `code_graph` ("what changed") | **review_pr_post** to flag community moves |
| Tier degradation warnings | `internal/tier/` | some tools, inconsistent | every tool (honest quality story) |
| prior_learnings | `internal/learnings/` | `understand`, `review_pr_post` (write) | ✓ already shared |

**Asymmetry summary:** 8 high-value AGE-computed signals are walled off behind the `code_graph` tool. No cost to expose them. Pure upside when DATABASE_URL is configured.

---

## Architecture — the `graphx` cooperation layer

New package `internal/graphx/` with two small interfaces and a no-op fallback. Nothing else about the stack changes.

```go
package graphx

// Analytics returns scalar signals computed by the persistent graph (AGE).
// Callers MUST tolerate zero values — the no-op backend returns them when
// AGE is unavailable or the graph is stale.
type Analytics interface {
    // Symbol returns pagerank, community ID, and surprise score.
    Symbol(ctx context.Context, repoKey, symbolName, file string) (Signals, error)
    // TopPageRank returns the K symbols with highest pagerank in the repo.
    TopPageRank(ctx context.Context, repoKey string, k int) ([]Signal, error)
}

type Signals struct {
    PageRank  float64
    Community string // empty when unassigned
    Surprise  float64
    Found     bool   // false = graph cold, signals are zero values
}

// CrossRefs surfaces edges that callgraph does not carry in-memory.
type CrossRefs interface {
    // HandlesRoute returns the Route a handler serves, if any.
    HandlesRoute(ctx context.Context, repoKey, symbolName, file string) (Route, bool, error)
    // FetchedBy returns frontend callers that FETCH a backend route.
    FetchedBy(ctx context.Context, repoKey string, route Route) ([]SymbolRef, error)
    // TestedBy returns test functions covering a production symbol.
    TestedBy(ctx context.Context, repoKey, symbolName, file string) ([]SymbolRef, error)
}
```

**Implementations:**

- `codegraph.Store` grows two thin methods (`AnalyticsAdapter`, `CrossRefsAdapter`) that wrap existing queries. Zero new SQL.
- `graphx.Noop{}` returns empty, never errors — used when `cfg.DatabaseURL == ""` or AGE is not ready.

**Wiring:**

- `analyze.Deps` gets two new optional fields: `Graph graphx.Analytics` and `Refs graphx.CrossRefs`. Both default to `Noop{}` in `register.go`. Same pattern as `Deps.Learnings`.
- Consumer tools take these from `Deps` and enrich output when `signals.Found` is true.

---

## Phases

Strictly sequential at phase level; tasks within a phase may run in parallel where indicated.

### Phase 1 — Define the interfaces and no-op fallback

#### Task 1 — Create `internal/graphx/` with interfaces + types + Noop
**Agent:** Sonnet. **TDD:** required.

Files:
- `internal/graphx/graphx.go` — interfaces, types (Signals, Signal, Route, SymbolRef), `Noop{}`.
- `internal/graphx/graphx_test.go` — table tests on `Noop` (must never error, always zero/empty).

Acceptance:
- `GOWORK=off go test ./internal/graphx/...` green.
- Package compiles with no dependency on `codegraph` or `callgraph` (pure types). This is what makes compound/impact able to import it without circularity.

Commit: `feat(graphx): introduce cooperation interfaces for graph signals`.

#### Task 2 — `codegraph.Store` implements both interfaces
**Agent:** Sonnet. **TDD:** integration test gated on DATABASE_URL (skip otherwise).

Files:
- `internal/codegraph/adapter.go` — `AnalyticsAdapter(store)` + `CrossRefsAdapter(store)` constructors; methods call existing Cypher templates (pagerank, community, surprise, HANDLES, FETCHES, TESTED_BY).
- `internal/codegraph/adapter_test.go` — against a real pgvector/AGE instance if `DATABASE_URL` is set; skips otherwise.

Acceptance:
- `codegraph.AnalyticsAdapter` satisfies `graphx.Analytics` (compile-time check via `var _ graphx.Analytics = (*adapter)(nil)`).
- Same for `CrossRefs`.
- Return `Signals{Found: false}` when the repo key has no snapshot.

Commit: `feat(codegraph): expose graph signals via graphx adapters`.

---

### Phase 2 — Wire `analyze.Deps`

#### Task 3 — Add `Graph` and `Refs` to `analyze.Deps`
**Agent:** Sonnet.

Files:
- `internal/analyze/deps.go` — add `Graph graphx.Analytics` and `Refs graphx.CrossRefs` fields.
- `cmd/go-code/register.go` — populate from `codegraph.Store` when DATABASE_URL is set; fall back to `graphx.Noop{}` otherwise. Mirror the pattern used for `Deps.Learnings`.

Acceptance:
- `mcp__go-code__understand` continues to work with and without DATABASE_URL (verified by smoke).
- Build + lint + tests green.

Commit: `feat(deps): expose graphx Analytics + CrossRefs to tools`.

---

### Phase 3 — Enrich existing tools with signals

Each task can run in parallel **only if** it touches disjoint files. Listing per task.

#### Task 4 — `understand` surfaces `<graph_analytics>`
**Agent:** Sonnet. Touches `internal/compound/understand.go`, `cmd/go-code/tool_understand.go`.

When `deps.Graph.Symbol(...)` returns `Found=true`, append a `<graph_analytics pagerank=".." community=".." surprise=".."/>` element to the XML result. Omit when `Found=false`. Update golden test fixtures.

Acceptance: existing `understand` XML unchanged when graph cold; new block present with fresh graph.

Commit: `feat(understand): surface pagerank/community/surprise when available`.

#### Task 5 — `prepare_change` flags community spread + high-PR callers
**Agent:** Sonnet. Touches `internal/compound/prepare_change.go`, `cmd/go-code/tool_prepare_change.go`.

For each direct caller, resolve analytics. In the output:
- Add `communities_crossed` = distinct community count across target + direct callers.
- Mark top-3 callers by pagerank as `<caller pagerank_tier="high"/>`; thresholds derived from `TopPageRank` p90.

Acceptance: when no graph, output is byte-identical to today. With graph, new attributes appear.

Commit: `feat(prepare_change): flag community spread and high-PR callers`.

#### Task 6 — `review_pr` and `review_pr_post` include surprise + community moves
**Agent:** Sonnet (two files but same concern). Touches `internal/review/delta.go`, `cmd/go-code/tool_review_pr.go`, `tool_review_pr_post.go`.

For each `ChangedSymbol`:
- Pull `Signals` before and after (before = from pre-snapshot via `codegraph.DiffGraphs`; after = current). Flag when community changed.
- Emit `<finding kind="community_move" before=".." after=".."/>` when detected.
- Persist as `learnings.Record.Flag="community_move"` through the existing write path.

Acceptance:
- On a PR that doesn't change community, no new finding.
- Manufactured case (integration test with fixture repo) emits exactly one community_move finding per moved symbol.

Commit: `feat(review): flag community moves and surprise spikes`.

#### Task 7 — `call_trace` follows `FETCHES` edges across languages
**Agent:** Sonnet. Touches `internal/callgraph/trace.go` (no direct `codegraph` import — call via `graphx.CrossRefs` passed in `TraceOpts`).

Extend `TraceOpts` with an optional `CrossRefs graphx.CrossRefs`. When non-nil and the trace reaches a symbol that `HANDLES` a Route, inject synthetic nodes for each `FetchedBy` hit (marked `kind=cross_language_fetch`). Depth-cap at 1 extra hop.

Acceptance:
- Without CrossRefs, `Trace` identical to today.
- With CrossRefs + a Go handler + a React fetch, the trace includes the React caller once.

Commit: `feat(call_trace): extend via cross-language FETCHES when available`.

#### Task 8 — `impact_analysis` counts tested_by coverage
**Agent:** Sonnet. Touches `internal/impact/impact.go`, `cmd/go-code/tool_impact.go`.

For the target symbol, pull `TestedBy` via `CrossRefs`. Add `tests_covering` (count) and `<tests>` list to the blast-radius XML. Useful signal on its own, stronger when paired with pagerank.

Acceptance: unchanged output when graph cold; new XML element when present.

Commit: `feat(impact): include test coverage for changed symbol`.

---

### Phase 4 — Uniform degradation story

#### Task 9 — Canonicalize `internal/tier/` warning emission
**Agent:** Sonnet. Touches `internal/tier/`, tool registrations.

Every tool handler currently decides ad-hoc whether to emit a `<tier>basic</tier>` warning. Extract a helper: `tier.Annotate(result, tier tier.Tier, reasons []string) string`. Enforce a single XML schema:
```xml
<quality tier="basic|enhanced|full">
  <degraded reason="no DATABASE_URL"/>
  <degraded reason="graph TTL expired"/>
</quality>
```

Audit all 27 tools for inconsistent tier emissions; fix three worst offenders identified by `mcp__go-code__code_search pattern="tier"`.

Acceptance: every tool that degrades announces it the same way; `mcp__go-code__repo_analyze` report score unchanged; no behavioural regressions.

Commit: `refactor(tier): uniform degradation emission across tools`.

#### Task 10 — Docs: `CLAUDE.md` ecosystem section
**Agent:** Haiku.

Add a new section to `CLAUDE.md` titled **"Graph signal ecosystem"** with a 6-row table describing which signals flow from where to where (`callgraph` → compound, `codegraph` → compound via graphx, etc.). Link to this plan.

Acceptance: `CLAUDE.md` within 200 lines; new section readable without opening other files.

Commit: `docs(CLAUDE.md): describe graph signal ecosystem`.

---

### Phase 5 — Smoke + rollout

#### Task 11 — End-to-end smoke + release memo
**(runs as described below)**

#### Task 12 — Consolidate `review_pr` + `review_pr_post` into one tool
**Agent:** Sonnet. **Runs AFTER Task 11** so smoke verifies both existing tools still produce correct graph signals before we collapse them.

**Rationale:** the two tools share ~90% of the pipeline (`review.DeltaReview`, policy application, findings rendering, graph enrichment). Separation was originally about read-vs-write intent, but a single tool with explicit `dry_run` semantics expresses the same intent with less surface area and one registration path. This is cleanup, not feature work — it only makes sense once Tasks 1-11 prove the graph-signals pipeline is correct on both paths.

**New unified shape:**
```
mcp__go-code__review_pr:
  repo: required
  pr: required
  depth: optional (int, default 2)
  dry_run: optional (bool, default true) — when true, returns review without posting
  event: optional ("APPROVE" | "COMMENT" | "REQUEST_CHANGES") — required when dry_run=false
```

- When `dry_run=true` (default) → today's `review_pr` behaviour, including graph flags.
- When `dry_run=false` + `event` set → today's `review_pr_post` behaviour: post to GitHub + persist `learnings.Record` (with `Flag`/`Note` from the graph enrichment).
- When `dry_run=false` without `event` → structured error `"event is required when dry_run=false"`.

**Files:**
- Modify: `cmd/go-code/tool_review_pr.go` — absorb the post path; add `DryRun` + `Event` to `ReviewPRInput`.
- Delete: `cmd/go-code/tool_review_pr_post.go`.
- Delete: `cmd/go-code/tool_review_pr_post_test.go` (if present) — port its cases into `tool_review_pr_test.go` under `t.Run("post/...")`.
- Modify: `cmd/go-code/register.go` — drop the second registration.
- Update `README.md` + `CLAUDE.md` — tool table: 27 → 26 tools.

**Compatibility:** existing Claude Code clients / hook scripts that call `mcp__go-code__review_pr_post` will need to migrate to `review_pr` with `dry_run=false`. Search `~/.claude/` for callers before deleting (`grep -rn review_pr_post ~/.claude/ ~/plugins/` on the controller side, not in the subagent — controller handles client migration). Since this is our own ecosystem and nobody external uses `review_pr_post`, the break is controlled.

**Tests:** merge `TestReviewPRPost_*` cases into `TestReviewPR_Post_*` subtests. The learnings persistence path must stay covered (existing `review_pr_post_learnings_test.go` style).

**Acceptance:**
- `mcp__go-code__review_pr_post` no longer registered — only `review_pr` remains.
- `review_pr dry_run=false event=COMMENT` behaves exactly as `review_pr_post event=COMMENT` today.
- `review_pr dry_run=true` (default) identical to today's `review_pr`.
- All tests green.
- MCP tool count 26 in `README.md` / `CLAUDE.md`.

**Why it's at Task 12 and not earlier:** if consolidation happened before Task 6, we'd be instrumenting graph signals into a moving target (two tools converging into one). Sequencing:
1. Tasks 1-5 build the plumbing.
2. Task 6 lands graph flags into both existing tools.
3. Tasks 7-11 finish ecosystem + smoke.
4. Task 12 collapses the two into one — lowest-risk point because both code paths are already verified.

**Rollback:** single revert restores both tools. No data migration required.

Commit: `refactor(review): consolidate review_pr + review_pr_post into a single tool with dry_run flag`.

---

### Phase 5 — Smoke + rollout (original Task 11)
**Agent:** Haiku.

1. `git push origin main` → dozor redeploys (background, timeout 600000).
2. Wait for `docker logs go-code --since 2m | grep ready`.
3. Run each enriched tool twice: once on a cold-graph repo (e.g. a newly cloned external repo), once on a warm-graph repo (`$REPO_ROOT` itself):
   - `understand` — confirm `<graph_analytics>` appears only when warm.
   - `prepare_change` — confirm `communities_crossed` appears only when warm.
   - `call_trace` — with and without CrossRefs.
   - `review_pr` — dry-run against an existing PR; confirm no regression.
4. Write `docs/memos/2026-04-18-graph-ecosystem-release.md` with before/after samples and the four smoke results.

Acceptance: memo exists, all four smoke cases pass visibly.

Commit: `docs(memos): graph ecosystem release log`.

---

## Dependency graph

```
Task 1 (graphx interfaces) ── Sonnet ── spec + quality review (Opus)
    ↓
Task 2 (codegraph adapter) ── Sonnet ── spec + quality review (Opus)
    ↓
Task 3 (Deps wiring)       ── Sonnet ── spec review
    ↓
Task 4─8 (tool enrichments) ─ Sonnet × 5, pairwise parallel-safe:
   • 4 understand   ┐
   • 5 prepare_change├─ all touch disjoint files; can run in parallel
   • 6 review       │   pairs → group into 2 waves to respect backend
   • 7 call_trace   │   subagents-serial rule (per CLAUDE.md)
   • 8 impact       ┘
   Each followed by spec review; Opus quality review on 4, 6, 7 (complex).
    ↓
Task 9 (tier canonicalization) ── Sonnet ── spec + quality review (Opus)
    ↓
Task 10 (docs) ── Haiku
    ↓
Task 11 (smoke + memo) ── Haiku
```

Backend subagents preference (from `feedback_backend_subagents_serial.md`) overrides naive parallelism: implement **Tasks 4-8 in two waves of 2-3 agents, not all five at once**, to avoid context pollution on review.

Wall-time estimate:
- 2 × Sonnet (Phase 1): ~1.5h
- 1 × Sonnet (Phase 2): ~30min
- 5 × Sonnet (Phase 3, two waves): ~3h
- 1 × Sonnet (Phase 4): ~1h
- 2 × Haiku: ~30min
- Reviews (Opus × 4, spec × 5): ~1.5h

**Total ~8h** end-to-end with reviews.

---

## Review protocol (per `feedback_subagent_driven_development.md`)

Two stages on Sonnet-implemented tasks:
1. **Spec review** — Sonnet checks deliverable against the task's **Acceptance** block only.
2. **Code quality review** — Opus checks:
   - File size ≤200 target / ≤300 hard.
   - No panic/unwrap in handlers.
   - `fmt.Errorf("context: %w", err)` pattern.
   - Interface boundaries: consumer tools never import `codegraph` directly post-refactor (only `graphx`).
   - Graceful degradation: every enriched output must produce byte-identical results when `graphx.Noop{}` is in use.

Haiku tasks skip Opus review.

---

## Rollback

Every task is a stand-alone commit. Reverts are safe at any seam because:
- `graphx.Analytics`/`CrossRefs` is optional — removing an enricher only drops a new output field.
- `Deps` only gains fields (no breaking change).
- No DB migrations, no schema changes — AGE schema unchanged; we only read via Cypher.

If Phase 2 (Deps wiring) breaks production: revert commit → everything falls back to the old path.

If an enrichment (Phase 3) emits bad XML: revert that specific tool's commit; the rest of the ecosystem stays intact.

---

## Success criteria

- `mcp__go-code__understand` on a warm-graph repo shows `<graph_analytics>`; on cold, doesn't.
- `mcp__go-code__prepare_change` shows `communities_crossed` when graph is present.
- `mcp__go-code__call_trace` includes at least one cross-language FETCHES hop on a repo that has Go handler + React fetch (oxpulse-chat qualifies).
- `mcp__go-code__review_pr_post` persists `community_move` findings and they resurface in future `understand` calls (proves the loop closes).
- No tool regresses in cold-graph mode — zero behavioural changes when `DATABASE_URL` is empty.
- `internal/tier` emits the same XML across all 27 tools.

---

## Why this order

Phase 1 + 2 are plumbing with no user-visible effect — safe to ship alone. If we stop there, nothing changes for users, no risk, and the foundation is in place.

Phase 3 is where the ecosystem starts helping itself. Each task is independently valuable: ship Task 4 (understand enrichment) alone and you get measurably more context in the PreToolUse hook without any other change, because the hook calls `prepare_change` which calls `understand`-like aggregation.

Phase 4 is cleanup — should only be done once Phase 3 proves the cooperation model works in anger.

Phase 5 closes the loop with a real release memo.

---

### Task 13 — Per-symbol surprise score (Option B)

**Agent:** Sonnet. **Runs AFTER Task 12** — adds per-symbol `surprise` on the persisted graph so `graphx.Signals.Surprise` stops being always-0.

**Rationale:** the existing `rankSurprises` in `internal/codegraph/surprise.go` scores CALLS edges on the fly when `code_graph` gets a "find hidden dependencies" query. Scores are not persisted — so compound tools (`understand`, `prepare_change`, `review_pr`) can't read them. Option A (compute per-query) was rejected: would cost ~30-100ms per adapter call, multiplied by N callers in `prepare_change`.

**Implementation:**

1. `internal/codegraph/surprise_index.go` (new, ~120 LOC):
   - `IndexSurpriseEdges(ctx, store, graphName) error` — runs one Cypher to fetch all CALLS edges joined with endpoint `pagerank`, `community`, `file`. Computes degree per endpoint via a second query (`MATCH (s)-[:CALLS]-() RETURN s.name, count(*)`). For each edge, calls `scoreSurprise` (existing). Writes `SET r.surprise = $score` in batches of `GRAPH_BATCH_SIZE` (AGE limit 5).
   - `IndexSurpriseNodes(ctx, store, graphName) error` — aggregates: for each Symbol, MAX of adjacent edge surprises, written as `SET s.surprise = $score` (normalized 0..1 via `score/6.0`).

2. `internal/codegraph/index.go` `IndexRepo`:
   - After existing pagerank + community passes, call `IndexSurpriseEdges` then `IndexSurpriseNodes`.
   - Gated behind env `CODEGRAPH_SURPRISE_INDEX=1` (default off for first deploy — turn on once perf measured on oxpulse-chat).

3. `internal/codegraph/adapter.go` `analyticsAdapter.Symbol`:
   - Extend Cypher to `RETURN s.pagerank, s.community, s.surprise`.
   - Populate `Signals.Surprise` from new column; 0 when NULL.

4. Tests:
   - `surprise_index_test.go` — fixture with a known cross-package edge, assert `r.surprise >= 3` and `s.surprise > 0` on endpoints. Gate on `DATABASE_URL`.
   - Update `adapter_test.go` to cover surprise in the unit-test stub and the live path.

5. Rollout:
   - Ship with `CODEGRAPH_SURPRISE_INDEX=0` first.
   - Measure index time on oxpulse-chat (expected ~2-5s post pagerank).
   - Flip to `=1` in `compose/search.yml` once cost is acceptable.
   - `code_graph refresh=true` on each warm repo to re-index with surprise.

**Acceptance:**
- `mcp__go-code__understand` on a symbol that crosses communities returns `GraphSignals.Surprise > 0`.
- `review_pr` on a PR touching such a symbol emits `high_surprise` flag (previously always dead-code because Surprise was 0).
- Cold-graph and flag-off path both preserve byte-identical output.

**Commit:** `feat(codegraph): persist per-symbol surprise during indexing`.

---

### Task 14 — Human-invoked `remember_graph_insights` tool + `/gc:remember` slash command

**Agent:** Sonnet. **Runs AFTER Task 13.**

**Rationale (revised):** earlier version wired persistence into `code_graph` itself, gated by env + optional per-query flag. **Rejected.** Agents call `code_graph` frequently and copy flags from examples mechanically — they have poor temporal sense for "this finding is worth saving permanently." Per-query flags become meaningless noise.

**New design:** `code_graph` stays **purely read-only**. Persistence lives in a **separate, explicitly-named MCP tool** (`remember_graph_insights`) that is NOT mentioned in any agent system prompt and is only reachable via a human-invoked slash command (`/gc:remember <repo>`). Agents can call `code_graph` a hundred times a day — zero persistence. When the human decides "bank current findings" they type `/gc:remember`, the tool fires once.

**Why this works:**
- Temporal sense delegated to human → typically weekly or post-merge gesture, not per-call noise.
- `code_graph` remains side-effect-free — agents can't accidentally trigger persistence.
- Permanent dedupe by `(repo, symbol, flag)`: second `/gc:remember` on the same findings is a no-op.
- No env gate needed — the tool's very existence is opt-in by the human typing the slash command.

**Implementation:**

1. `internal/codegraph/persist_insights.go` (new, ~120 LOC):
   ```go
   package codegraph
   
   import (
       "context"
       "github.com/anatolykoptev/go-code/internal/learnings"
   )
   
   const minSurpriseScoreForPersist = 5
   
   // PersistInsights writes per-symbol Records to the learnings store based on
   // the template and rows of a QueryResult. No-op when store is nil.
   // Returns count of records written.
   func PersistInsights(ctx context.Context, store *learnings.Store, repoKey, template string, rows [][]string) int { ... }
   ```
   Dispatch by template:
   - `surprises` (or freeform rows that match the surprise row shape of 6 cols): parse via a helper that mirrors `postProcessSurprises`; for each edge with `score >= minSurpriseScoreForPersist`, write 2 Records (from/to symbols) with `Flag="hidden_dep"`, `Note=reasons joined`.
   - `dead_code`: one Record per row with `Flag="dead_code_candidate"`.
   - `community_changes` (when "what changed in the graph" is used): one Record per moved symbol with `Flag="community_move"`, `Note="community changed X → Y"`.
   - Everything else (`who_calls`, `call_chain`, `most_connected`, `complex_functions`, etc.) → skip (point queries, not structural insights).
   Dedupe: before writing, check existing Records via `Nearest(repoKey, symbol, 1)` — if same Flag within last 24h, skip.

2. `cmd/go-code/tool_code_graph.go` `registerCodeGraph` handler:
   - After `QueryGraph` returns successfully, if `cfg.PersistInsights && deps.Learnings != nil`, call `codegraph.PersistInsights(ctx, deps.Learnings, input.Repo, result.Template, result.Rows)`.
   - Log count: `slog.Info("code_graph: persisted insights", "template", t, "count", n)`.
   - Errors swallowed — never fail the user-visible query because persistence failed.

3. `cmd/go-code/config.go`:
   - Parse `CODEGRAPH_PERSIST_INSIGHTS` (bool, default false).

4. Tests:
   - `persist_insights_test.go` — stub `learnings.Store` (interface-extract if needed); assert for each template:
     - `surprises` with score=3 → 0 records (below threshold)
     - `surprises` with score=5 + 2 endpoints → 2 records
     - `dead_code` with 3 rows → 3 records
     - `who_calls` with rows → 0 records
     - Dedupe path: second call within 24h → 0 records

**Constraints:**
- Cold path (flag off OR store nil) MUST preserve today's behaviour byte-for-byte.
- Never error the `code_graph` response because of persistence failure.
- Flag `persisted_count` in the XML response metadata when writes happened (so users see the hook fired).
- Threshold `minSurpriseScoreForPersist = 5` — scoreSurprise max is 6, so score≥5 means at least 4 of 5 reasons matched. Tunable later.

**Acceptance:**
- Call `code_graph` on oxpulse-chat with "find hidden dependencies" → log shows "persisted insights count=N".
- `psql -c "SELECT count(*) FROM review_learnings WHERE flag='hidden_dep'"` returns N.
- Subsequent `understand` on one of those symbols → `<prior_learnings>` contains the hidden_dep flag.
- Second identical query within 24h → count=0 (dedupe works).

**Commit:** `feat(codegraph): persist findings to learnings store when flag set`.

**Rollback:** Single revert. No schema change (reuses existing `review_learnings` table).

---

## Out of scope (deliberately)

- **Merging `callgraph` and `codegraph` packages** — they have legitimately different lifecycles (ephemeral vs persistent). Merging adds complexity without removing duplication (because the extraction layer is already shared).
- **Moving AGE queries to the in-memory side** — AGE's analytics (pagerank, community) require persistence of snapshots; re-implementing Louvain in-memory is a rabbit hole with no payoff.
- **Changing the extraction pipeline** — parser → callgraph is canonical and works well.
- **Other potential "shared modules"** — `internal/embeddings/` and `internal/forge/` are already properly factored. No work needed.
