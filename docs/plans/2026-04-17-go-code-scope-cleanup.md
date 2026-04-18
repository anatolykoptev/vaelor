# go-code Scope Cleanup — Refocus, Don't Migrate

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development`. Each task lists its subagent tier (Haiku/Sonnet/Opus).

**Goal:** Refocus go-code by removing paths that duplicate `go-wowa` tool surface while **preserving the unique reverse-engineering pipeline** that was the original design intent of `site_analyze`.

## Context — results of deep inventory

Before drafting this plan we did a full read-through of every candidate tool + package + test + the original design docs (`docs/plans/2026-03-06-site-analyze-design.md`, `docs/plans/2026-03-28-site-audit-upgrade.md`). Findings:

1. **`site_analyze mode=full` is a code-intelligence pipeline**, not a web tool. Original design statement:
   > "Analyze any website's technology stack and extract frontend source code for further analysis with existing go-code tools (explore, symbol_search, dep_graph)."
   It fetches deployed JS → parses `//# sourceMappingURL=` → downloads maps → writes reconstructed source tree to `{WorkspaceDir}/sites/{domain}/` → returns a path consumed by go-code's own `explore`/`symbol_search`/`dep_graph`. This is unique; go-wowa has no analog and cannot easily gain one (no synthetic-repo concept).

2. **`site_analyze mode=detect`** is a pure forwarder to ox-browser `/analyze`, formatted as XML. 100% duplicate of `go-wowa.analyze` (minus XML wrapping). Safe to drop.

3. **`site_crawl`** is a pure forwarder to ox-browser `/crawl`, no downstream code-intel pipeline depends on its output. 100% duplicate of `go-wowa.crawl`. Safe to delete.

4. **`internal/webanalyze/` package** has four layers:
   - `Client.Analyze` + `Client.Fetch` + `AnalyzeResponse.Assets` — **needed by mode=full** to discover script URLs.
   - `Client.Crawl` + `CrawlInput/Response` + `parseSSECrawl` — used **only** by `site_crawl`. Delete with it.
   - `sourcemap.go` (`ParseSourceMap`, `WriteSourceTree`, `FindSourceMapURL`, `sanitizePath`) — **unique** code-intel logic, no equivalent anywhere. Keep.
   - `types.go` — 188 lines of SEO/Perf/A11y/Content/Media/Fonts/PWA/API reports. Consumed **only** by `formatDetectResponse`. Delete.

5. **`wp_plugin_search`** returns `wp:slug` identifiers that go-code's `ingest.SearchWPPlugins` + cloner read to analyze plugin source. Code-intel discovery layer. Keep.

6. **`design_search`** + `design_helpers.go` — semantic search over design docs via the same `EMBED_URL` stack (`jina-code-v2`). Code-adjacent. Keep.

7. **`code_search_oxcodes`** — not a standalone tool, it's the scoped/structural dispatch inside `code_search` to ox-codes. Keep.

## Scope verdict table

| Action | Target | Reason |
|---|---|---|
| **Narrow** `site_analyze` → only `mode=full` (remove `mode` param, always do reverse-engineering) | `cmd/go-code/tool_site_analyze.go` + `_format.go` | Delete duplicated detect path; keep unique code-intel pipeline |
| **Delete** `site_crawl` | `cmd/go-code/tool_site_crawl.go` | Pure duplicate of `go-wowa.crawl` |
| **Trim** `internal/webanalyze/` to `Analyze` + `Fetch` + source-map logic | `client.go`, `types.go` | Remove crawl + detect-formatter types |
| **Rename** package `internal/webanalyze/` → `internal/sourcemap/` | package + imports | Name reflects the surviving responsibility |
| **Keep as is** | `wp_plugin_search`, `design_search`, `code_search_oxcodes`, `webhook_github` | Genuine code-intel |
| **No changes** to go-wowa | — | No feature needs porting — nothing is being lost |

**Net diff target:** ~−380 LOC in go-code, 0 LOC in go-wowa, tool count 27→26 (site_crawl only).

**Execution mode:** Subagent-driven, strictly sequential (backend — per `feedback_backend_subagents_serial.md`). Two-stage review per coding task: spec adherence → code quality.

**Repo:** `/path/to/repos/src/go-code` (branch `main`, dozor auto-deploys on push).

---

## Task 1 — Design-doc snapshot (protect against feature loss)

**Agent:** Haiku. Read-only, no code changed.

**Deliverable:** `docs/memos/2026-04-17-scope-cleanup-inventory.md` containing:

1. Full-text of `site_analyze mode=full` pipeline with line-accurate references.
2. List of **every public symbol** in `internal/webanalyze/` with consumers (use `mcp__go-code__symbol_search` + `call_trace`).
3. Explicit list of types in `types.go` that are referenced by anything other than `tool_site_analyze_format.go`. If the list is empty, record that fact.
4. Evidence (file+line) that `site_crawl` has zero downstream code-intel consumers.

**Why:** Acts as the canonical "what we must not lose" document. Every subsequent task must check its outputs against this memo.

**Acceptance:** Memo committed; grep in the memo confirms `mode=full` pipeline is captured end-to-end.

---

## Task 2 — Narrow `site_analyze` to code-intel-only

**Agent:** Sonnet.

**Scope:** Collapse the two-mode tool into a single-purpose "reverse-engineer a deployed site into a synthetic repo". Drop SEO/Perf/A11y/etc formatting; drop the `mode` field.

**Files to modify:**
- `cmd/go-code/tool_site_analyze.go`:
  - Remove `SiteAnalyzeInput.Mode`, constants, and `mode == "detect"` branch.
  - Update tool description: "Reverse-engineer a deployed site: download JS, extract source maps, write original sources to a local directory usable by `explore` / `symbol_search` / `dep_graph`."
  - Keep `handleSiteAnalyze` → `extractSources` → `formatFullResponse` chain.
- Delete `cmd/go-code/tool_site_analyze_format.go` in full. The only survivor — `formatFullResponse` — moves (trimmed, without SEO/perf helpers) into `tool_site_analyze.go`.
- `cmd/go-code/register.go`: handler signature may simplify (no change to function name `registerSiteAnalyze`).

**TDD:** Add `cmd/go-code/tool_site_analyze_test.go` (if not present) with one table test calling `handleSiteAnalyze` against a fake `*webanalyze.Client` that returns a fixture `AnalyzeResponse{Assets:{Scripts:[...]}}` + fake Fetch responses containing a known sourceMappingURL comment. Assert the response XML contains `<sources>` block and the mock workdir contains reconstructed files. Start with the test failing against the old code (because it has `mode` requirement), update implementation to pass.

**Preflight (subagent git hygiene):**
1. `git status --short` — if dirty, STOP.
2. Stage only the three files listed above.
3. After staging, `git status --short` — refuse if extras present.

**Steps:**
1. Write failing test.
2. Narrow `SiteAnalyzeInput` + handler + format helpers.
3. `GOWORK=off make test && make lint`. Must be green.
4. Commit: `refactor(site_analyze): drop detect mode, focus on source-map reverse engineering`.

**Acceptance:**
- Test passes.
- `mcp__go-code__symbol_search repo=/path/to/repos/src/go-code query=formatDetectResponse` → 0 hits.
- `mcp__go-code__symbol_search ... query=extractSources` → still present.
- Lint + build green.

---

## Task 3 — Delete `site_crawl` + crawl code path in webanalyze

**Agent:** Sonnet.

**Files to delete:**
- `cmd/go-code/tool_site_crawl.go`
- Any sibling test file (check `tool_site_crawl_test.go`).
- From `internal/webanalyze/client.go`: delete `crawlTimeout`, `CrawlInput` usage, `Client.Crawl`, `parseSSECrawl`.
- From `internal/webanalyze/types.go`: delete `CrawlInput`, `CrawlResponse`, `CrawlPage`, `CrawlSummary` and any crawl-only helpers.

**Files to modify:**
- `cmd/go-code/register.go`: drop `registerSiteCrawl` call.

**Preflight:** same as Task 2.

**Steps:**
1. Delete files and symbols.
2. `GOWORK=off go build ./...` → all references must be gone. Fix any stragglers.
3. `make test && make lint`. Green.
4. Commit: `refactor: remove site_crawl (duplicate of go-wowa.crawl)`.

**Acceptance:**
- `rg -n 'site_crawl|parseSSECrawl|CrawlInput' cmd/go-code internal/webanalyze` → 0 hits.
- Build + tests + lint green.

---

## Task 4 — Trim `webanalyze/types.go` to minimum surface

**Agent:** Sonnet.

**Scope:** After Task 2 lands, most types in `types.go` become unused. Delete them; keep only what `site_analyze mode=full` still needs.

**Keep (based on Task 1 inventory):**
- `Technology`, `Meta` — tolerable (exercised in a small XML summary).
- `Assets{Scripts, Stylesheets}` — **required**.
- `AnalyzeResponse` — keep only fields that `extractSources`/`formatFullResponse` read. Drop `SEO`, `Performance`, `Accessibility`, `Content`, `Media`, `Fonts`, `PWA`, `API`, `Method`, `CFDetected`, `ElapsedMs`.
- `FetchResponse` — required.

**Delete all the other report structs.**

Update `internal/webanalyze/client_test.go` — remove assertions on trimmed fields.

**Steps:**
1. Delete the unused types.
2. Trim `AnalyzeResponse` fields.
3. Run `GOWORK=off go vet ./...` + `make test`. Must pass.
4. Commit: `refactor(webanalyze): trim unused report types`.

**Acceptance:**
- `rg -n 'SeoReport|PerformanceReport|AccessibilityReport|PwaReport|ApiReport|FontsReport|ContentReport|MediaReport' internal/webanalyze` → 0 hits.
- Line count of `types.go` ~≤50.

---

## Task 5 — Rename package `webanalyze` → `sourcemap`

**Agent:** Sonnet. Pure rename, low risk after Tasks 2-4.

**Rationale:** Package name no longer reflects its sole surviving responsibility (source-map extraction). `sourcemap` is accurate and short.

**Steps:**
1. `git mv internal/webanalyze internal/sourcemap`.
2. Replace `package webanalyze` → `package sourcemap` in all files.
3. Replace `webanalyze.X` → `sourcemap.X` across callers (only `tool_site_analyze.go` after previous tasks).
4. Replace import path `.../internal/webanalyze` → `.../internal/sourcemap` everywhere.
5. `GOWORK=off go build ./... && make test && make lint`.
6. Commit: `refactor: rename internal/webanalyze → internal/sourcemap`.

**Acceptance:** `rg -n webanalyze .` → 0 hits in source files. Tests green.

---

## Task 6 — Docs + CLAUDE.md + README

**Agent:** Haiku.

**Files to modify:**
- `README.md`:
  - Update tool count (27 → 26).
  - In the feature list, restate `site_analyze` as "Reverse-engineer deployed frontends via source maps" (not "Website tech analysis").
  - Remove any mention of `site_crawl`.
- `CLAUDE.md`:
  - Replace row `internal/webanalyze/` → `internal/sourcemap/` with description "Source-map → synthetic repo pipeline for `site_analyze`".
  - Update the MCP tool row for `site_analyze`.
  - Remove `site_crawl` row.
- `docs/memos/2026-04-17-scope-cleanup-release.md`: summary of before/after, rationale, link to Task 1 inventory memo.

**Acceptance:** `grep -rn site_crawl README.md CLAUDE.md` → 0 hits. `grep -rn webanalyze README.md CLAUDE.md` → 0 hits.

Commit: `docs: refocus site_analyze story, drop site_crawl references`.

---

## Task 7 — Verify via go-code MCP + deploy

**Agent:** Haiku.

**Steps:**
1. `git push origin main` (run in background per `feedback_deploy_background.md`, timeout 600000) → dozor redeploys.
2. Wait; verify `docker logs go-code --since 2m | grep -i ready`.
3. Post-deploy smoke:
   - `mcp__go-code__explore repo=/path/to/repos/src/go-code` → `packages` list must not contain `internal/webanalyze`; should contain `internal/sourcemap`. File count is down by ~3.
   - `mcp__go-code__symbol_search repo=/path/to/repos/src/go-code query=site_crawl` → 0 hits.
   - `mcp__go-code__symbol_search repo=/path/to/repos/src/go-code query=site_analyze` → still 1 hit.
   - `mcp__go-code__site_analyze url=https://react.dev` against a known source-mapped site — should either return `<sources files="N">` with N>0 or the "No source maps found" fallback (both acceptable; we only check that the tool is callable and doesn't 500).
4. Append findings to `docs/memos/2026-04-17-scope-cleanup-release.md` under **## Verification**.

Commit: `docs(memos): scope-cleanup verification`.

**Acceptance:** Memo has all four smoke results. No panics, no 500s in logs.

---

## Dependency graph

```
Task 1 (inventory, Haiku — read-only)
    ↓
Task 2 (narrow site_analyze, Sonnet)  ── spec review → code-quality review (Opus)
    ↓
Task 3 (delete site_crawl, Sonnet)    ── spec review → code-quality review (Opus)
    ↓
Task 4 (trim types, Sonnet)           ── spec review (Sonnet), no Opus — mechanical
    ↓
Task 5 (rename package, Sonnet)       ── spec review only — mechanical
    ↓
Task 6 (docs, Haiku)
    ↓
Task 7 (deploy + smoke, Haiku)
```

**Strictly sequential.** Every task touches the same files (`client.go`, `types.go`, `tool_site_analyze.go`, `register.go`) so parallelism is unsafe (explicit user preference per `feedback_backend_subagents_serial.md`).

**Wall-time estimate:** 4 Haiku tasks (~20 min avg), 4 Sonnet tasks (~45 min avg), 2 Opus reviews (~20 min each). Total with reviews ~4-5h.

---

## Review protocol

After each Sonnet task (2, 3):
1. **Spec-adherence review** — Sonnet reviewer checks deliverable against **Acceptance** block only.
2. **Code-quality review** — Opus reviewer checks:
   - ≤200 lines/file target (hard ≤300).
   - No panic/unwrap in handlers.
   - `fmt.Errorf("context: %w", err)` pattern.
   - No silent errors.
   - No unused exports left behind.

Tasks 4, 5 skip Opus review (mechanical refactors; Sonnet spec check enough). Tasks 1, 6, 7 need no review gate.

---

## Rollback

Each task is an independent revertable commit. If any task breaks production, `git revert <sha> && git push origin main` restores previous behaviour. No DB or schema migrations involved.

---

## Success criteria (final)

- `mcp__go-code__explore ...` shows `internal/sourcemap` (renamed), no `internal/webanalyze`.
- `mcp__go-code__symbol_search ... query=site_crawl` → 0.
- `mcp__go-code__symbol_search ... query=SeoReport` → 0.
- `site_analyze` description in tools listing mentions "source maps" / "reverse-engineering", not "SEO" / "accessibility".
- README no longer lists `site_crawl` under features.
- All original `mode=full` functionality works end-to-end (smoke in Task 7).

## Why NOT port anything to go-wowa

The original migration idea (port source-maps extractor to `go-wowa.analyze`) was rejected after deep inventory revealed:
- `site_analyze` writes to `{WorkspaceDir}/sites/{domain}/`. That directory is a go-code concept; go-wowa doesn't have it.
- The returned path is used by go-code's own `explore`/`symbol_search`/`dep_graph`. Putting this in go-wowa means go-wowa output is consumed by a different service — violates bounded-context boundary.
- No unique feature is actually lost by keeping `site_analyze` in go-code — only duplication is removed.

The principle: **move ownership to where the consumer lives, not to where the data originates.**
