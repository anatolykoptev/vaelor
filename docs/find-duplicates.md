# find-duplicates — Design, Validation & Ship Decision

**Phase 5 updated 2026-06-02. Phase 4 baseline below.**

---

## 1. What was built (Phases 1-5)

### Engine (`internal/embeddings`)
- `FindNearDuplicates` (Phase 5) — scalable per-symbol HNSW k-NN candidate generator (N × O(log N)). For each symbol, calls `Store.Search` with the symbol's own embedding as a constant query vector (EXPLAIN-proven to use `idx_code_embeddings_hnsw`). Deduplicates via canonical pair key (lesser endpoint is A). Returns `NearDupResult{Pairs, SearchErrors}` where `SearchErrors > 0` signals an incomplete run. **Replaces `FindSimilarPairs` in `AnalyzeTriage`.**
- `FindSimilarPairs` — pgvector O(n²) self-join (kept; `Analyze` grade-ratio path still uses it for small repos).
- `FindExactDuplicates` — fast index-equality scan on `(repo_key, body_hash)` partial index; no vector distance needed.
- `PairsConnectedByCalls` / `PairsSharingInterface` — batch AGE Cypher queries used by the filter chain.

### Filter chain (`internal/semhealth/dupfilter.go`)
Cheap pure filters run first; AGE graph filters last:

| Filter | What it drops |
|--------|--------------|
| `tests` | pairs where either endpoint is a test file (`langutil.IsTestFile`) |
| `same_file` | pairs where both endpoints share a file path (disabled by `IncludeSameFile=true`) |
| `kind` | pairs where either endpoint has a low-signal kind: `field`, `var`, `const`, `import` |
| `calls_edge` | pairs with a CALLS edge in either direction in the AGE graph |
| `interface_sibling` | pairs where both endpoints implement the same interface node |

### `AnalyzeTriage` (`internal/semhealth/triage.go`)
Combines exact + similar tiers into a `TriageResult`:
- `Groups` — merged, sorted by tier rank (exact > very-close > related) then `AvgSimilarity` desc.
- `Candidates` — raw pair count before filters.
- `Dropped` — per-filter drop counts.
- `ReportedByTier` — group count per tier.
- `TimedOut` (Phase 5) — true when `FindNearDuplicates` reported `SearchErrors > 0` or returned a fatal error. Operators must not interpret `TimedOut=true` as "no duplicates found" — the result is partial.

Returns `&TriageResult{}` (not nil) when `totalFuncs > semhealthMaxFuncs=5000`.

### Two surfaces
1. **`code_health focus=semantic_duplicates`** — tiered report embedded in the quality grade, filtered and labeled, for everyday review.
2. **`find_duplicates` MCP tool** — operator triage: full group list with similarity, tier, file paths; designed for manual review and refactor targeting.

---

## 2. Tier thresholds

| Tier | Similarity | Cosine distance | Rationale |
|------|-----------|----------------|-----------|
| `exact` | 1.0 (body hash) | 0 | Textual clone — FNV-64a hash equality; zero FP from embedding noise |
| `very-close` | ≥ 0.88 | ≤ 0.12 | Near-copy — same logic with minor naming/comment differences |
| `related` | ≥ 0.80 | ≤ 0.20 | Structural clone — same pattern, distinct vocabulary; highest FP rate |

The `related` tier is the widest band and the primary noise source; the filter chain is essential at this tier.

---

## 3. Live validation results

**Test date:** 2026-06-02  
**Harness:** `internal/semhealth/dup_validation_test.go` (build tag `integration`)  
**Command:**
```
DUP_TEST_DSN=postgresql://gocode_app:***@127.0.0.1:5432/gocode \
DUP_TEST_REPO_KEY=code_bb3c1bea \
GOWORK=off CGO_ENABLED=1 go test -tags=integration -run TestDupValidation -v \
  ./internal/semhealth/...
```

### Primary target: `code_bb3c1bea` (234 functions, TypeScript)

This is a TypeScript mesh-networking library (`agent-fw` style). Preferred because it is small enough for the 15 s statement_timeout, has an AGE graph, and produced the cleanest results.

| Metric | Value |
|--------|-------|
| Total functions indexed | 234 |
| Candidates (pre-filter) | 50 (capped by `maxSimilarLimit=200`) |
| Dropped — `same_file` | 26 |
| Dropped — `tests` | 0 |
| Dropped — `kind` | 0 |
| Dropped — `calls_edge` | 0 |
| Dropped — `interface_sibling` | 0 |
| Reported groups (`very-close`) | 7 |
| Reported groups (`related`) | 8 |
| Reported groups (`exact`) | 0 |
| **Total groups reported** | **15** |
| Elapsed | 378 ms |
| Filter-invariant result | **PASS** |

### Secondary target: `code_87ce8eca` (300 functions, Go)

A Go security-probe service (`go-pentest`). Confirms filter chain behaviour on a Go repo with AGE graph activity.

| Metric | Value |
|--------|-------|
| Total functions indexed | 300 |
| Candidates (pre-filter) | 27 |
| Dropped — `same_file` | 15 |
| Dropped — `calls_edge` | 2 |
| Dropped — `interface_sibling` | 0 |
| Reported groups (`very-close`) | 1 |
| Reported groups (`related`) | 7 |
| Total groups reported | 8 |
| Elapsed | 621 ms |
| Filter-invariant result | **PASS** |

The `calls_edge=2` drop is direct evidence that the AGE graph filter ran on real data and removed two caller/callee pairs that the vector similarity surface alone would have flagged.

### Filter invariants verified

For every group and symbol:
- No endpoint is a test file.
- Every group has a valid tier string in `{exact, very-close, related}`.
- Every `DupSymbol.Kind` is non-empty.

For every cross-file pair in reported groups (re-queried against the live AGE graph):
- `PairsConnectedByCalls` returned 0 connected pairs.
- `PairsSharingInterface` returned 0 sibling pairs.

**All assertions passed on both targets.**

### Note on union-find and same-file group members

`CollectDupGroups` runs union-find over the **filtered** pair set. A group of 3 can contain two symbols from the same file if they are each independently cross-file-similar to a third symbol (e.g. `A(f1)↔C(f2)` and `B(f1)↔C(f2)` produces group `{A,B,C}` where A,B share f1). The same-file pair `A↔B` was never in the filtered input — it is a transitive union-find artifact. This is expected; the filter invariant is about the **input pair set**, not about all pairwise cross-products of group members. Observed in `code_87ce8eca` group [6] (`parseRequestHeader` and `parseRequest`, both in `parse_config.go`, merged via `render_widget.go:parseRequestOffsets`). The test documents and skips transitive same-file pairs for the AGE re-check.

### Top-10 precision sample — `code_bb3c1bea` (human assessment)

| Rank | Tier | Sim | Symbols | Assessment |
|------|------|-----|---------|------------|
| 0 | very-close | 0.964 | `makeNonce` × 2 | session.ts vs session-ratchet.ts — **real dup**: identical helper, copy-paste across two crypto session implementations |
| 1 | very-close | 0.922 | `decrypt` × 2 | session.ts vs session-ratchet.ts — **real dup**: same decrypt logic duplicated |
| 2 | very-close | 0.919 | `encrypt` × 2 | session.ts vs session-ratchet.ts — **real dup**: same pattern as decrypt |
| 3 | very-close | 0.909 | `hex`/`toHex`/`bytesToHex` × 3 | three files — **real dup**: three inline byte-to-hex utilities, classic "copy-paste utility" pattern |
| 4 | very-close | 0.899 | `close` × 4 | inbox/dedup-bloom/outbox/spool — **partial dup**: same `close()` lifecycle pattern across 4 mailbox implementations; may be correct design (interface sibling) — filter did not remove it, worth reviewing |
| 5 | very-close | 0.886 | `constructor` × 2 | inbox vs outbox — **partial dup**: structurally similar constructors; may be genuine dup or deliberate symmetric design |
| 6 | very-close | 0.876 | `evictOlderThan` × 3 | spool/outbox/inbox — **real dup**: eviction policy duplicated across mailbox types |
| 7 | related | 0.876 | `evictExcess` × 2 | inbox vs spool — **plausible dup**: same eviction contract |
| 8 | related | 0.872 | `getDb` × 2 | inbox vs outbox — **plausible dup**: same pattern |
| 9 | related | 0.869 | `uint8ToBase64Url`/`toBase64url` | transport vs base64url — **real dup**: inline base64url utility vs a proper library function |

**Honest read:** 7–8 of the top 10 groups look like genuine duplicates worth consolidating. Groups 4 and 5 (`close` × 4 and `constructor` × 2) are ambiguous — they may be correct interface implementations that the `interface_sibling` filter missed because the AGE IMPLEMENTS edges for TypeScript were not indexed for this repo. The `related` tier (groups 7-9) is noisier but still plausible.

**Noise rate estimate for top-10:** ≤ 2/10 = 20% FP at this repo. The `very-close` tier alone (7 groups) has an estimated 0-1 FP.

---

## 4. Limitations (known at ship time)

### (a) Vaelor self-dogfood deferred

Vaelor itself (`github.com/anatolykoptev/vaelor`) is not currently indexed in `code_embeddings`. The embed backend (`embed.krolik.tools`) was saturated and timing-out during this feature's development, and the `code_embeddings` table was wiped. Indexing Vaelor is the highest-value validation because reinvention of helpers (e.g. multiple `pgError` extractors, multiple `retry` patterns) is the exact target class.

**Operator action after embed-server recovers:** run `find_duplicates repo=vaelor` to complete the dogfood validation. No code changes needed.

### (b) Scalability — RESOLVED in Phase 5

**Status: RESOLVED.** Phase 5 replaced the O(n²) all-pairs self-join with per-symbol HNSW k-NN.

**Phase 5 approach:** `FindNearDuplicates` issues N individual `Store.Search` calls, each with the symbol's own embedding as a constant query vector. `EXPLAIN` on a constant-vector query with `WHERE repo_key=$1` shows `Index Scan using idx_code_embeddings_hnsw`, cost 732 (vs 1.6M for the LATERAL correlated join, which also does NOT use the index — confirmed by `EXPLAIN`). N × ~17ms = ~35s total for 2085 symbols, well within any reasonable timeout.

**The LATERAL option (A) was rejected:** `EXPLAIN` of a correlated self-join with `a.embedding` as the distance argument produces `Limit → Sort(embedding <=> a.embedding) → Bitmap Heap Scan`, cost 1.6M — pgvector does not use HNSW for correlated per-row vectors. Only a constant query vector uses the HNSW index.

**Live re-validation on `code_f40acc09` (memdb-go, 2085 funcs) — 2026-06-02:**

| Metric | Value |
|--------|-------|
| Total functions indexed | 2085 |
| Candidates (pre-filter) | 381 |
| TimedOut | false |
| Dropped — same_file | 146 |
| Dropped — calls_edge | 12 |
| Reported groups (very-close) | 36 |
| Reported groups (related) | 69 |
| Total groups reported | 105 |
| Elapsed | ~37s |
| Filter-invariant result | PASS |

Previously this repo timed out at 15 s with 0 candidates (silent failure). Now it completes in ~37s with 381 candidates and 105 groups.

**Remaining semantic difference vs all-pairs:** `FindNearDuplicates` returns each symbol's top-k nearest neighbours (k=5 by default). A symbol with more than 5 near-duplicates surfaces only its 5 closest. This is intentional and documented — in practice the 5-nearest is sufficient for actionable refactor targets, and k can be raised by callers for exhaustive analysis.

**5000-function size guard:** `semhealthMaxFuncs=5000` is retained for result-size bounding (the exact tier is index-cheap but is kept inside the guard for consistency). The guard is no longer a scalability requirement for the similar-tier path.

### (c) `filterKind` is currently inert on real data

The embed pipeline only indexes `function` and `method` kinds. The `field`, `var`, `const`, `import` kinds that `filterKind` guards against are never present in `code_embeddings`. Drop count is 0 on all observed runs. This is expected and documented — the filter is forward-defensive for future parser improvements that may emit other kinds.

### (d) Transitive same-file members in merged groups

Documented in §3 above. Not a bug — the filter operates on pairs; union-find operates on the filtered set. A post-processing step that drops group members leaving the group with only same-file companions would improve output quality but is not critical.

---

## 5. Ship decision

**Recommendation: SHIP (Phase 5 strengthens the case).**

**Evidence supporting ship (updated for Phase 5):**
- Filter invariants hold on three live repos: `code_bb3c1bea` (234 funcs, TS), `code_87ce8eca` (300 funcs, Go), `code_f40acc09` (2085 funcs, Go/Python).
- Phase 5 resolves the blocking scalability limitation: `code_f40acc09` previously timed out silently with 0 candidates; now completes in ~37s with 381 candidates and 105 groups.
- `TimedOut` field surfaces incomplete runs — operators can no longer mistake "timed out" for "no duplicates".
- `gocode_semhealth_dup_timeout_total` counter makes the previously-silent timeout observable in Prometheus.
- Top-10 precision on `code_f40acc09` shows high-quality real duplicates: `SearchLTMByVectorSQL` duplicated across `queries_memory_ltm.go` and `postgres_memory_ltm.go`, `Ping` duplicated across two Redis clients, `isCyrillic`/`isCJK` duplicated across tokenizer and lang packages.
- `calls_edge` filter demonstrably works: 12 pairs dropped on `code_f40acc09`.

**Remaining caveat (operator step, not a blocking concern):**
The Vaelor dogfood run is DEFERRED. Once `embed.krolik.tools` recovers and Vaelor is re-indexed, run `find_duplicates repo=vaelor` to confirm the tool finds the known reinvented helpers (multiple retry patterns, multiple pgError extractors, etc.).

**What would change the recommendation to SHELVE:**
- If the Vaelor dogfood run shows > 50% false positives in the `related` tier AND the `very-close` tier also degrades.

**Future work:**
1. Post-processing: after union-find, remove group members that are same-file as another member AND have no cross-file pair in the filtered set (cleans up transitive same-file artifacts).
2. IMPLEMENTS edge indexing for TypeScript/Python — would let `interface_sibling` filter remove protocol-pattern false positives (the `close × 4` group in `code_bb3c1bea`).
3. AGE re-check in validation test: the name-based Cypher re-check can produce false positives on large repos (name collision where A→C→B through a third node that shares names). Replace with a per-pair direct edge query for production-grade validation.
