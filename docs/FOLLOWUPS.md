# go-code Followups

Tracked items that surfaced during operations but don't block production.

## 2026-05-12 — Stale repo paths in eager warm

**Resolution (2026-05-25):** demoted the per-repo prewarm-failure log from WARN to DEBUG in `internal/callgraph/eager_warm.go`. Prewarm is best-effort and self-heals via lazy warm, so a failing `go build` for a non-buildable repo (dozor inconsistent-vendoring, etsy-forge) is expected, non-actionable noise — real failures still surface at query time. A skip-list (`skip_paths_extra`) remains the cleaner long-term fix if cold-start CPU ever matters.

**Symptom (logs):**
```
WARN msg="eager warm: prewarm failed" root=/host/src/etsy-forge err="exit status 1"
WARN msg="eager warm: prewarm failed" root=/host/src/dozor err="exit status 1"
```

**Context:** After hypervisor reboot + full image rebuild, go-code started and tried to prewarm 35 repos. Two failed because their source paths no longer exist (or `go build` errors out). Non-fatal — `eager warm` is best-effort, but produces noise in logs and wastes CPU attempting each rebuild.

**Suspected causes:**
- `etsy-forge` — appears to be archived/removed from active workspace; reference still in autoindex config
- `dozor` — repo present at `/home/krolik/src/dozor` but `go build` fails (likely needs build_paths/skip_paths config or has broken vendor state)

**Fix candidates:**
1. Audit `repos:` autoindex config — drop `etsy-forge` if no longer maintained
2. For `dozor`: add to skip_paths_extra OR diagnose `go build -mod=vendor ./...` failure cwd-side
3. Bonus: bump eager warm log level for non-existent root from WARN → DEBUG (less noise; missing repo is operator's intent)

**Where to look:**
- Autoindex config: ~/src/go-code (search `autoindex_repos` / `repos` setting)
- Per-repo: `~/src/etsy-forge` (verify exists), `~/src/dozor` (try `cd ~/src/dozor && go build -mod=vendor ./...`)

## 2026-05-12 — CPU cap added (150%)

**Change:** `~/deploy/krolik-server/compose/search.yml` go-code service got `deploy.resources.limits.cpus: '1.5'`.

**Why:** Cold-start eager warm spawns parallel `go build ./...` per repo (35 repos at parallelism=2). On 4-core ARM this can spike CPU 200+%, starving other containers (embed-server hit its 3.0 cap; system processes queued). Cap leaves headroom.

**Trade-off:** Cold-start warmup ~2× slower (single-threaded build per repo path) — acceptable; cold start is rare.

**Validation:** After cap applied 2026-05-12, LA should peak <12 during cold start (vs 18 unbounded).

---

## Phase 1 repowise port — smoke test bugs (2026-05-29)

Discovered after live smoke on PR #156 / v1.20.0. All non-blocking, but degrade signal quality.

### BUG-FH-1 (HIGH) — `get_file_health` returns non-source files in top-20 hotspots
**Where:** `cmd/go-code/tool_file_health.go::topHotspotPaths`
**Evidence:** smoke on acme-web returned:
- `docs/superpowers/plans/*.md` (4 entries, score 5 each) — markdown plans, churn high by nature, defect risk null
- `Cargo.lock` (7), `package-lock.json` (2/6), `pnpm-lock.yaml` (2), `test-e2e/package-lock.json` (5) — auto-generated lock files
- `web/static/audio/c2dec.js` + `c2enc.js` (5 each) — compiled codec2 WASM

**Why:** `compare.CollectChurn` returns all tracked files. No type/dir filtering.

**Fix:** Allow-list source extensions (`.go .rs .ts .tsx .js .jsx .svelte .py .java .kt .swift .rb .cs .cpp .c .h .hpp .php`) + deny-list dir prefixes (`vendor/ node_modules/ dist/ build/ static/ docs/ .claude/`) in `topHotspotPaths` before truncating to top-20.

### BUG-FH-2 (MEDIUM) — `get_file_health` duration_ms=11549 on 20 paths
**Where:** `cmd/go-code/tool_file_health.go::handleFileHealthCore` → 20 sequential `PriorDefect.Score` calls = 20× `git log --since=180.days`
**Evidence:** smoke duration 11.5s for 20 paths
**Fix:** Phase 2 — batch git query (`git log --pretty=%H|%s --since=180.days --name-only -- .` once, parse per-file) instead of per-path call.

### BUG-SR-1 (LOW) — `suggest_reviewers` returns co-change=0 for paths with obvious coupling
**Where:** `cmd/go-code/tool_suggest_reviewers.go::scoreFileReviewers` → `compare.CollectCoupling(ctx, root, suggestReviewersMinCoChanges=2)`
**Evidence:** smoke on `cmd/go-code/tool_dead_code.go` + `internal/compare/churn.go` returned co-change=0 even though both files have known co-change partners (other tool_*.go files).
**Hypothesis:** floor `minCoChanges=2` filters out recently-introduced pairs; possibly correct but slow signal warm-up.
**Fix:** Phase 2 — verify on multi-author repo (acme-web) with established co-change history. If coupling cache stale, force refresh.

### BUG-FH-3 (LOW) — top-5 cap returns only 1 suggestion on single-author repo
**Where:** `cmd/go-code/tool_suggest_reviewers.go`
**Evidence:** go-code repo (single author Anatoly) → 1 suggestion. Not a bug per se, but expose UX confusion when consumer expects "5".
**Fix:** Phase 2 — document the contract: "returns ≤5 distinct authors found in history".

### Verified WORKING

- Envelope footer `<!-- meta: {"duration_ms":N,"hint":"..."} -->` emitted on 5 retrofitted tools (verified via `code_search`)
- `HintAfterCodeSearch` silent on multi-hit (5 results) ✓
- `HintAfterCodeSearch` fires on single-hit declaration ✓ — `"single hit — call understand(symbol=\"defaultHealthRegistry\") for the body"`
- `ExtractSymbolFromHit` strips trailing `(` from `func defaultHealthRegistry()` correctly ✓
- `get_file_health` top file hint fires at score ≥7 ✓ — `"top file crates/signaling/src/rooms.rs scored 9/10..."`
- Hotspot detection identifies real bug-class files (rooms.rs, useCall.svelte.ts, useGroupCall.svelte.ts, register.rs) — all known [[acme-web-turn-loopback-score-regression]], [[feedback_svelte5_hydration_double_mount]], [[acme-web-turn-tls-url-resync-bug]] hotspots


---

## Phase 2b smoke test (2026-05-30) — all 5 items verified in prod

PR #158 / f7a0cb5 deployed + smoke-verified on acme-web:
- Commits=1 fix: ranking shifted (i18n translations, main.rs, analytics/mod.rs now in top-20 by true commit frequency)
- since window: ALL churn_risk reasons now say "in last 90 days" (was "across history")
- churn growth: analytics/mod.rs churn 88.9x (4976 lines / 56 LOC grown) now scores 5 (was 0 pre-fix)
- WithFreshness wiring: LIVE — code_search on /home/krolik/src/go-code emitted `stale_warning: index built against main 669a023, main is now f7a0cb5 -- call code_graph refresh=true`. Compares main-tip correctly (not working-tree HEAD).

### NEW FOLLOWUP — BUG-FH-2b (MEDIUM): get_file_health cold latency regressed 8.9s to 34s
**Where:** internal/biomarkers/churn_risk.go::initialCreationLines
**Cause:** Phase 2b added a per-file git log --diff-filter=A --reverse --since=90.days spawn (one per scored file). Cold get_file_health on 20 files = 20 extra git spawns on top of CollectChurn + BatchPriorDefect. Warm = 212ms (CollectChurn cached), but initialCreationLines is NOT cached/batched.
**Fix (Phase 2c):** batch initialCreationLines the way BatchPriorDefect batches prior_defect — one git log --diff-filter=A --name-only for all paths, attribute first-add per path. OR cache per (root, relPath) with the churn TTL.
**Severity:** MEDIUM — cold path only, bounded by 90-day window + 15s per-file timeout; warm is fast. Acceptable trade-off for the growth-fix correctness gain, but should be batched before get_file_health becomes a hot path in PR review.

---

## Phase 3a.1 signal-quality (2026-05-30) — followups from review

Shipped: origin dedup (chat/chat-dev collapse via slugparse), lift/confidence ranking with bucket-level co-occurrence, sw.js artifact filter. Three followups surfaced in code-quality review:

- **FU-2.1 (Phase 3a.2, MEDIUM) — replace smoothed lift with Dunning log-likelihood G². ✅ DONE.** `liftSmoothingAlpha` deleted; ranking is now a **two-tier sort** — well-supported pairs (`co_changes >= minConfidentSupport=3`) always outrank low-support pairs, then by G² (`internal/federate/significance.go`). The support tier was required because G² *alone* over-rewards perfect rare coincidences (a 2-window always-together pair scores higher G² than a loose-but-frequent genuine coupling); the tier guarantees a genuine co=8 coupling outranks a co=2 coincidence regardless of G². `lift` dropped from the wire (foot-gun — an LLM consumer could re-sort on it). Remaining nuance (FU-2.3, LOW): G² is scale-invariant at tiny N (ties fall to co-change count) and the χ²(df=1) thresholds don't correct for multiple comparison over `maxCrossPairs=20`; a Wilson-LB or FDR pass could refine, but the support tier resolves the operational mis-ranking.
- **FU-2.2 (LOW) — federate co-change tests are wall-clock-sensitive via `--since=365 days`.** `collectTouches` bounds history to 365 days; the regression tests use fixed 2026 dates (earliest 2026-01-05), so once wall-clock passes ~2027-01-05 the earliest windows silently drop, shrinking N and eventually perturbing the genuine/noise lift margin. Pre-existing class (all federate co-change tests), now load-bearing for `TestCrossRepoCoChange_RanksGenuineAboveNoiseAndCoincidence`. Harden by injecting a fixed "now"/`--until` or anchoring dates relative to `time.Now()`.
- **FU-1.1 (LOW) — thread request ctx into ResolveRepos for cancellable origin dedup.** `dedupeByOrigin` calls `gitutil.OriginURL(context.Background(), root)` per repo, so the per-repo 5s timeouts are NOT cancellable by the MCP request context (unlike `CrossRepoCoChange` which honours ctx). `git remote get-url` is a network-free `.git/config` read (~ms), so realistic latency is fine; the only risk is a non-cancellable tail if git wedges. Thread ctx through `ResolveRepos` (signature change) in a later pass.

---

## Phase 3a.3 — IDF + Wilson-LB ranking (2026-05-30)

Ported clean-room from CodeScene (sum-of-coupling/stop-word noise) + Evan Miller ("How Not To Sort By Average Rating", Wilson score). Research dossier: `reports/go-code/research/2026-05-30-change-coupling-detection-landscape.md`.

- **✅ DONE — rank by Wilson lower-bound on directional confidence; pre-filter ubiquitous files.** `Score = wilsonLowerBound(co, min(winA,winB), 1.96)` (`internal/federate/scoring.go`) — support-aware, continuous, never saturates (fixes the G² χ² label ceiling). `confidenceLevel` derives from the Wilson score so it discriminates again. Ubiquitous stop-word files (touched in >85% of windows — CHANGELOG/lockfiles) are dropped by `isUbiquitous` before scoring (fixes meta-file noise, no allowlist). The G²+two-tier sort, both support-caps, and `minConfidentSupport` are deleted; G²/significance remain informational fields.
- **REJECTED — the `sqrt(idf(A)·idf(B))` MULTIPLIER from the research sequencing.** Tried it first; code-quality review + mutation test proved it RE-INTRODUCES rare-coincidence inflation (a thin co=2 coincidence of rare files gets an IDF boost that beats Wilson's support penalty — genuine co=8 score 0.20 vs coincidence 0.69). IDF as a *score multiplier* over-rewards file rarity; IDF as a *binary stop-word filter* (the `isUbiquitous` form) keeps the noise-kill without the inflation. Lesson: ubiquity belongs as a filter, not a ranking weight, because genuine couplings are often between active (high-frequency) files.
- **FU-3.1 (NIT) — `ubiquityPct=85` is a hardcoded ranking knob** (`scoring.go`), documented with rationale but no env-gate/metric. Making it configurable is the clean follow-up if field tuning is ever needed. No correctness/security impact.
- **NEXT (Phase B, the differentiator) — semantic verify.** Temporal Wilson-LB is now a cheap *candidate generator*; the real signal jump is a two-stage `candidate → verify` on the top-K using go-code's EXISTING capabilities: offline `routes.ExtractAll` server↔client route match (repo A `Side=server POST /api/x` ↔ repo B `Side=client` same method+path) = a PROVEN provider↔consumer dependency, then AGE HANDLES/FETCHES + shared-symbol + embeddings as graph-gated enrichment. CHANGELOG-class noise vanishes by construction (no shared route/symbol → no evidence). Design doc: `plans/go-code/2026-05-30-federated-cochange-semantic-verify-architecture.md`. `routes` is pure-Go regexp (no CGO) → offline tier is cheap.

---

## Phase B — Semantic route-match verification (2026-05-30)

The differentiator: stage-2 verifier that PROVES a cross-repo dependency instead of guessing it from timing. Design doc: `plans/go-code/2026-05-30-federated-cochange-semantic-verify-architecture.md`.

- **✅ DONE — offline T0 route-match verifier (`internal/coupling`).** For each temporal candidate pair, reads both files' bytes, runs `routes.ExtractAll` (pure-Go regexp, no CGO, no DATABASE_URL), and emits `Evidence{kind:"route", detail:"POST /api/partner/register", tier:"offline"}` when a `Side=server` route in one file matches a `Side=client` route in the other by method + `NormalizePath`. `VerifyPairs` wraps each `federate.CrossPair` → `VerifiedPair{verified, linkedBy, evidence}`, sorts verified-first then by temporal Score; the tool emits `[]VerifiedPair`. **Resolves P1/P2 by construction**: CHANGELOG/README/lockfiles define no route → no evidence → `verified:false`, sorted below proven pairs (no threshold); "how strong" becomes "what link was proven" (categorical, non-saturating). Temporal Wilson-LB is now the cheap stage-1 candidate generator. Generic-path guard added (skip `/health`/`/metrics`/single-segment paths — cross-service collisions, not real links).
- **FU-B.1 (Phase C) — T1 shared-symbol verifier (offline).** Reuses the bytes already read for T0. Catches non-HTTP couplings: shared exported const / env-var token / protocol field / route-literal string (e.g. acme `RELAY_JWT_SECRET`). Without it, some genuine non-route pairs stay `verified:false` (retained, sorted below verified — recall preserved, only the label differs). Conservative token stop-list (skip `Error`/`Config`/short tokens).
- **FU-B.2 (Phase C) — T2 AGE graph-confirm enrichment.** When `DATABASE_URL` is set, `codegraph` `HandlesRoute`/`FetchedBy` via `graphx.CrossRefs` interface upgrades a T0 match to `graph_route` confidence (confirm A's handler HANDLES the route, B's symbol FETCHES it). Inject as interface, `graphx.Noop` cold-path → byte-identical-shape guarantee without DB. Per-repo graph (no cross-repo Cypher — corroborates, doesn't discover).
- **FU-B.3 (Phase D) — T3 embeddings soft cosine fallback + commit-message ticket-ID linkage (CodeScene's strongest signal — same `OXP-204` in both repos' commit bodies) + directional lead-lag (does A precede B?).** Lowest priority — after C proves value.
- **FU-B.4 (NIT) — `routeKey` now duplicated** in `internal/coupling/route_verify.go` + `internal/compare/routediff.go` (unexported there). At a 3rd consumer, promote `RouteKey` into `internal/routes` and collapse both.

---

## Phase C — T1 shared-symbol verifier (offline) (2026-05-30)

The real signal for acme: cross-repo coupling flows through shared protocol constants / WS message-type literals / env-var names — NOT HTTP routes (Phase B's T0 returned `VERIFIED: 0` on real acme-* because the coupling is WebSocket/WebRTC, not REST). T1 catches it. Plan: `plans/go-code/2026-05-30-phaseC-shared-symbol-verify.md`.

- **✅ DONE — offline T1 `symbolVerifier` + `compositeVerifier` (`internal/coupling`).** `extractSignificantSymbols` (language-agnostic byte-scan, no CGO) pulls two structurally-rare token classes — `SCREAMING_SNAKE` consts/env-vars (≥1 underscore) and structured snake/kebab string literals (≥1 separator, e.g. `"peer_joined"`). `symbolVerifier` intersects the two files' token sets and emits `Evidence{kind:"symbol", tier:"offline"}` per shared token (sorted, capped 5). `compositeVerifier` runs T0 (route) + T1 (symbol) and concatenates evidence (route first → `LinkedBy` prefers the more-specific route proof); a failing sub-verifier never sinks another's evidence, and partial evidence survives a soft error (future I/O tiers). Wired live into `federated_cochange`. E2E handler test proves handler→composite→symbol reaches `verified:true` on a `RELAY_JWT_SECRET`+`peer_joined` fixture. Reuses `readVerifyFile` (shared with T0 — `lang==""` gate skips markdown/lockfile/VERSION noise by construction, so release-noise can never verify).
- **✅ DONE (MAJOR fix) — generic-infra-suffix floor.** Without it, a coincidental shared infra env-var (`DATABASE_URL`/`REDIS_URL`/`LOG_LEVEL`) flipped unrelated files to `verified:true` and — because the sort floats verified above all unverified regardless of temporal Score — floated noise above genuine coupling (inverting the feature's promise). `isGenericInfraToken` drops tokens whose tail is an infra-config suffix (`_url`/`_host`/`_port`/`_addr`/`_level`/`_timeout`/`_retries`/`_endpoint`/`_region`/...). Secret-safe: EXCLUDES `_secret`/`_key`/`_token`/`_password` so `RELAY_JWT_SECRET` (the primary target) survives. The symbol-path analog of T0's `isGenericRoute`.
- **FU-C.1 (LOW) — true zero-IO shared-byte cache.** T0 and T1 each read every candidate file once (separate per-(root,rel) caches) → one redundant `os.ReadFile` per file, served from page cache (bounded by `maxCrossPairs=20` → ≤80 reads). A composite that reads once and passes bytes to sub-verifiers would eliminate it but requires changing the `Verifier` interface to accept bytes. Deferred — page-cache-cheap today; revisit if a 3rd I/O tier lands.
- **FU-C.2 (MINOR) — generic-infra suffix list is a precision/recall knob.** `_endpoint`/`_region` are the two suffixes most likely to collide with a genuine shared protocol token (e.g. `SIGNALING_ENDPOINT`, `PARTNER_REGION` would be dropped). Acceptable trade today (a false-positive drowning real signal is worse than a missed edge — T1 is one of several verifiers, a miss degrades to temporal score not a wrong answer). If a real acme coupling-miss is ever traced to a dropped `*_ENDPOINT`, the clean fix is a structure-based discriminator (require the *value* to be an infra DSN/URL shape), NOT expanding the suffix list. Watch the slope: 15 entries is fine; past ~25 or domain-specific (non-infra-vocabulary) suffixes → revisit. For the record: `_scheme`/`_proto`/`_version`/`_namespace` were deliberately NOT included (protocol-ish, not infra).
- **FU-C.3 (Phase D) — per-symbol strength weighting.** All shared tokens currently weigh equally for the `verified` flag. A longer/rarer shared token (a 3-segment domain secret) is stronger proof than a 2-segment generic literal; feeding token specificity into a symbol-aware confidence field would refine ranking once T2/T3 land. Also: `maxSymbolEvidence=5` silently caps emitted tokens — fine for a proof signal (5 shared contract names is ample), but a "+N more" affordance would need an `Evidence` struct change.
- **NEXT — T2 AGE enrichment (FU-B.2)** remains the next tier when `DATABASE_URL` is set: graph-confirm a route/symbol match via `HANDLES`/`FETCHES`. T1 (offline) is now the baseline default-correct path for acme's non-HTTP coupling. **BUT see the Codegraph section below — T2 is dead until the route→graph edge-builder is fixed (HANDLES/FETCHES=0 fleet-wide today).**

---

## Codegraph route→graph layer — FLEET-WIDE breakage (2026-05-30 investigation)

Surfaced while diagnosing why `federated_cochange` T2 (AGE graph-confirm) is dead on acme. The root is NOT federated_cochange-specific — the cross-language route→graph layer is broken across the whole fleet, affecting `code_graph`, `call_trace` (cross-language fetch nodes), and `dep_graph` (`cross_language=true` edges). Live evidence: **only `code_68f8014a` (go-nerv) has any HANDLES/FETCHES edges (4/48); all 16 other repo graphs — including go-code itself — have HANDLES=0/FETCHES=0**. Plan (architect T2/T3 design, partially refuted by this investigation — route-quality is secondary, the edge-builder is primary): `plans/go-code/2026-05-30-phaseD-graph-embed-enrich.md`. Investigation was diagnose-only; nothing fixed.

- **FU-CG.1 (CERTAIN, biggest lever) — edge-builder skips every route with an empty Handler.** `internal/codegraph/index_layers.go:262` `for _, r := range routeList { if r.Handler == "" { continue } ... }` — HANDLES/FETCHES emitted ONLY when `r.Handler != ""`. But the TS matcher (`internal/routes/match_typescript.go:25-27`, `tsRouterMethodRe` group 4) captures a handler ONLY for `app.get("/p", namedFn)`; arrow/inline callbacks and ALL `fetch`/Nest routes get `Handler=""`. So ~100% of non-Go routes produce zero edges. The lone success (go-nerv) matches the code's own Wave-6 constraint (`handlesFromKey` doc): "handler defined in same file as route registration (typical Go pattern)." **Fix:** (a) populate `Route.Handler` from the enclosing function (tree-sitter scope lookup — accurate, heavier), OR (b) build a **file-level** HANDLES edge (Symbol-in-file → Route) when no named handler resolves (the code's own "Wave 7+" TODO at index_layers.go:210-213). Independent of route-quality — go-code's own clean routes still have HANDLES=0, proving the empty-Handler skip (not junk routes) is the cause.
- **FU-CG.2 (CERTAIN, secondary) — Route vertices are junk: TS matcher over-matches.** `tsRouterMethodRe` matches `headers.get('Authorization')`, `map.get('/A')`, any object with an HTTP-verb-named method; `tsFetchRe` matches `fetch('/api/leak?c='+cookie)` from an XSS test fixture. acme-web has 94 Route nodes, mostly garbage (`/Authorization`, `/api/leak?c=`). NO ingest-time filter — `isGenericRoute` lives only in the query-time coupling verifier (`internal/coupling/route_verify.go:30`), <2-segment only, so 2-segment junk survives. **Fix:** constrain receiver to an allow-list (`app`/`router`/`r`/`fastify`/`server`), exclude `__tests__/`/`*.test.*`, add an ingest-time junk filter at `index_layers.go extractRoutes`. Also fixes T0 route-verifier precision.
- **FU-CG.3 (POSSIBLE, minor) — colon-in-path breaks the Route MATCH.** Edge ToKey = `Method+":"+Path`; `unwindEdgeMatch("Route")` (`internal/codegraph/cypher_batch.go:258`) splits ToKey on `:` → paths with `:` (`/peer1:unknown`) split wrong → no edge. ~3/94 acme-web paths — amplifier not driver. **Fix:** split on first `:` only, or use a `\x00` delimiter.
- **FU-CG.4 — Route vertex does not store `Side` (server/client).** `buildCrossLanguageGraph` Route props = method/path/framework only; no `side`. Even with edges, the graph can't distinguish server vs client route → T2 cross-repo provider↔consumer confirm is inexpressible. Add `side` to Route vertex props.
- **FU-CG.5 — Rust matcher handler capture unverified.** acme-edge (Rust) has Route=0 entirely (different gap than TS junk). `internal/routes/match_rust.go` handler capture not inspected — Rust routes may not be extracted at all. Verify before assuming the fix is TS-only.
- **FU-CG.6 (observability, project-rule) — silent data-loss at the empty-Handler skip.** `index_layers.go:262`'s `continue` discards routes with no counter — "0 HANDLES across 16/17 graphs" was invisible. Add `codegraph_routes_extracted_total{repo,framework,side}`, `codegraph_route_edges_built_total{repo,label}`, `codegraph_route_handler_empty_total{repo}` at line 262, `codegraph_graph_build_total{repo,status}` (58 `code_repo_state` rows vs 17 AGE graphs is invisible). Per the metrics-are-an-assert rule, the empty-Handler `continue` MUST bump a counter.
- **Ops** — after FU-CG.1/.2 land, reindex acme-web/acme-edge/admin/sfu-kit (force AGE rebuild; `code_5947b7f0` admin + `code_2eb0212a` sfu-kit have `code_repo_state` rows but missing/partial graphs).

### Codegraph route-edge fix — SHIPPED 2026-05-30 (CG-T1..T7), residual gaps

The route→graph fix (PR: feat/codegraph-route-edge-fix) landed observability scaffold, route-quality (junk/test filter + receiver allow-list + optional-param), the hybrid enclosing-fn resolver, `\x00` colon-safe keys, Route `side` prop (3-part key), `graph_build_total`, and the axum Rust matcher. HANDLES/FETCHES now build for Go-server / TS / Rust(axum) / PHP / HTML. **Residual gaps surfaced by the final integration review (NOT regressions — these were always zero-edge; the new `route_handler_unresolved_total` counter now makes them VISIBLE per-repo, so a non-zero unresolved floor is EXPECTED, not a new bug):**

- **FU-CG.7 (MAJOR scope — next wave) — 4 languages + Go-client still zero edges.** `internal/routes/match_go.go` (client `http.Get`/`http.NewRequest`), `match_csharp.go`, `match_java.go`, `match_python.go`, `match_ruby.go` emit routes with `Handler=""` AND `Line=0`. The CG-T3 enclosing-fn resolver needs a non-zero `Line` (line 0 matches no symbol span) → every such route falls to `route_handler_unresolved` and its edge is dropped. Fix: add `Line` capture (the `FindAllSubmatchIndex` + `lineAt` pattern from `match_typescript.go`/`match_rust.go`) to these 5 matchers, then the resolver lights them up too. Until then, expect `route_handler_unresolved_total{repo}` > 0 on Go-client-heavy / C#/Java/Python/Ruby repos — that's the counter doing its job, not an alarm.
- **FU-CG.8 (MINOR) — side-blind read queries.** `adapter_crossrefs.go:102` `FetchedBy` + `templates.go:209,216,223` (incl. `all_hooks`) MATCH `Route{method,path}`/`{framework,path}` WITHOUT `side`. Correctness holds today only because the `[:HANDLES]`/`[:FETCHES]` edge-label discriminates (FETCHES attaches exclusively to client-side Route vertices). Latent fragility — add `side` to these MATCH patterns for defense-in-depth. Note `all_hooks` now emits 2 rows where a same-named server+client wordpress hook exists (arguably more-correct, but a behavior change).
- **FU-CG.9 (followup counter) — `edges_built` counts intent, not graph outcome.** `recordRouteEdgeBuilt` fires at Go-side edge construction (`index_layers.go:434`), BEFORE the AGE MATCH. A built edge whose endpoint MATCH finds nothing (e.g. cross-file handler: registration file ≠ definition file, so `handler\x00routeFile` ≠ the Symbol's `handler\x00defFile`) inflates `edges_built` without a real graph edge AND bumps no failure counter. Add an "edge built but endpoint unmatched" signal (or move `edges_built` to post-MATCH) so `extracted = edges_built + rejected + unresolved` is a truthful partition. The 3-part-key change widened this class (side must now also agree).

---

## Phase 5 — frontend-parse-parity lock-in (2026-07-01)

Two right-sized follow-ups deliberately deferred out of the parity arc
(`plans/go-code/2026-06-30-frontend-parse-parity-react-svelte-astro.md`, ADR
`docs/adr/0001-frontend-parse-parity.md`) rather than folded into it — each
touches live, already-working walkers/call sites and carries independent
regression risk that would have widened the arc's blast radius for no new
capability.

- **FU-P5.1 (LOW) — byte-walker consolidation.** Three independent byte-walkers implement variants of the same "find matching brace / skip quoted strings" logic: `StripGoTemplate` (`internal/parser/preproc/gotemplate.go:39`), `collectSkipRanges` (`internal/parser/preproc/astro.go:100`), and the Phase-1 `matchBrace` + `skipQuoted` helper introduced for the markup-expr scanner (`internal/parser/preproc/braces.go:22`). Retrofit `StripGoTemplate` and `collectSkipRanges` onto the shared `matchBrace`/`skipQuoted` helper so there is one delimiter-matching primitive instead of three. Deferred because Phase 1's own reflexion round already hit a real per-caller behavioral difference (`skipQuoted`'s escape-handling flag: `false` for Go raw-string backtick semantics vs `true` for JS/TSX braces) — retrofitting the two *live* walkers (go-template, htmx/astro) onto the shared helper needs the same care, and doing it inside this arc would have expanded the parity harness's job to also golden-guard go-template/astro output it doesn't otherwise touch.

  **Investigated (2026-07-01), not landed — premise doesn't hold on inspection.** Re-read all three walkers before touching code: (1) `StripGoTemplate`'s quote-skipping is *already* routed through the shared `skipQuoted` primitive — `findActionClose` (`gotemplate.go:147`) calls `skipDoubleQuoted`/`skipBacktickQuoted`, both thin wrappers over `skipQuoted` with the correct per-caller `escaped` flag (`true` for `"..."`, `false` for `` `...` ``) — so that half of the consolidation shipped implicitly with `quotes.go`'s own doc comment already claiming it. What's left unshared is `findActionClose` itself, and it is not a `matchBrace` variant: `matchBrace` matches a *single* `{`/`}` pair by nesting depth, with no concept of two-char delimiters. `findActionClose` finds the first closing `}}` (or trim-marked `-}}`) for a two-char `{{`/`}}` action, with no depth tracking at all — nesting of `{{if}}...{{end}}` blocks is handled one layer up in `StripGoTemplate`'s own keyword-based `blockFrame` stack, not by brace counting. Retrofitting it onto `matchBrace` would mean generalizing `matchBrace`'s contract to accept delimiter pairs, trim markers, and `{{/* */}}` comments — a real expansion of the *live* Astro/Svelte `{expr}` scanner's own primitive, not a pure DRY move, and exactly the blast-radius growth this arc structured Phase 5 to avoid. (2) `collectSkipRanges` (`astro.go:100`) does no brace-matching or quote-skipping at all — it finds `<script>...</script>` / `<style>...</style>` tag-pair boundaries via `bytes.Index` substring search. There is no shared algorithm with `matchBrace`/`skipQuoted` to consolidate onto; forcing a retrofit here would mean inventing a new HTML-tag-boundary primitive under the `matchBrace` name, not reusing the existing one, while touching the live `ExtractAstroWithRefs`/`scanTemplateRefs` producer. Conclusion: this FU's own text overstated the overlap between the three walkers — quote-skipping is already unified via `skipQuoted`'s `escaped` flag, and the two remaining pieces (`{{`/`}}` action-close-finding, `<script>`/`<style>` tag-pair-finding) are structurally different problems from `matchBrace`'s single-char nested-depth matching. Left open rather than force-fit; if a future pass wants a single primitive here, it needs a new delimiter-pair-aware scanner (not `matchBrace` itself), sized and golden-tested as its own arc.

- **FU-P5.2 (LOW) — shared `parseTree` helper.** Six call sites in `internal/parser` repeat the same `sitter.NewParser()` → `SetLanguage` → `ParseCtx` → (use tree) → `tree.Close()`/`p.Close()` boilerplate: `internal/parser/calls.go:81` (`ExtractCalls`'s raw-`CallsQuery` path), `internal/parser/markup_calls.go:65` (`scriptRegionCalls`) and `:106` (`markupExprReparse`), `internal/parser/parser_base.go:61` (`parserBase.Parse`), `internal/parser/relationships.go:49` (`ExtractRelationships`), and `internal/parser/runes_svelte.go:23` (rune classifier). Factor a shared `parseTree(lang *sitter.Language, code []byte) (*sitter.Node, func(), error)` (or equivalent) to collapse the repeated setup/teardown into one place. Deferred as a separate, right-sized follow-up — Phase 5 is tests + docs + assertions only, no production-code behavior changes, and this consolidation touches every call-extraction and relationship-extraction path in the parser, which belongs in its own reviewed PR with its own golden-diff proof of byte-identical behavior (mirroring how the Phase 1 `parseVirtualWithRemap` extraction was golden-tested).

  **Resolution (2026-07-01):** landed as specified — `parseTree(lang *sitter.Language, code []byte) (root *sitter.Node, closeFn func(), err error)` added in `internal/parser/parser_base.go`, next to `mustCompileQuery`; `closeFn` closes the tree then the parser, matching every original site's defer order exactly. All six production call sites converted (`calls.go`, `markup_calls.go` ×2, `parser_base.go`, `relationships.go`, `runes_svelte.go`); `context`/`sitter` imports dropped where they became unused (`calls.go`, `markup_calls.go`, `relationships.go`, `runes_svelte.go`). Byte-identical proof: `TestTSLangRemapGolden` (golden file unchanged in `git diff`), `TestMarkupParity`/`TestNoDuplicateMarkupEdges`, and the full `internal/parser` + `internal/parser/preproc` + `internal/callgraph` suites all green post-refactor. **PR #275 review follow-up:** the 7th `package parser` instance of the same boilerplate, `firstDestructurePattern` (`internal/parser/runes_destructure_unit_test.go:20-27`, test-only), was also converted onto `parseTree` — it was directly callable (same package, unexported helper) and carried zero shipped-behavior risk. `internal/compare/astdiff.go:70-83` (`ComputeASTDiff`) is a KNOWN, DELIBERATELY-EXCLUDED site: different package (`compare`, not `parser` — `parseTree` is unexported) AND it reuses ONE `*sitter.Parser` across TWO `ParseCtx` calls (treeA, treeB), a real structural mismatch against `parseTree`'s one-parser-per-call contract, not a drop-in swap. A future reader should not assume repo-wide elimination of this boilerplate shape from this follow-up.
