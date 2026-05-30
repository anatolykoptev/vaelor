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
