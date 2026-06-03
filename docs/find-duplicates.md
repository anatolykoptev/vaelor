# find-duplicates — Design, Validation & Ship Decision

**Phase 4 decision doc. Written 2026-06-02 against live DB results.**

---

## 1. What was built (Phases 1-3b)

### Engine (`internal/embeddings`)
- `FindSimilarPairs` — pgvector O(n²) self-join with 15 s `statement_timeout` guard; returns `(symbol, file, line, kind, similarity)` pairs.
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

`CollectDupGroups` runs union-find over the **filtered** pair set. A group of 3 can contain two symbols from the same file if they are each independently cross-file-similar to a third symbol (e.g. `A(f1)↔C(f2)` and `B(f1)↔C(f2)` produces group `{A,B,C}` where A,B share f1). The same-file pair `A↔B` was never in the filtered input — it is a transitive union-find artifact. This is expected; the filter invariant is about the **input pair set**, not about all pairwise cross-products of group members. Observed in `code_87ce8eca` group [6] (`parseClientHelloHeader` and `parseClientHello`, both in `vless_ja3.go`, merged via `probe_ech.go:parseClientHelloOffsets`). The test documents and skips transitive same-file pairs for the AGE re-check.

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

### (a) go-code self-dogfood deferred

go-code itself (`github.com/anatolykoptev/go-code`) is not currently indexed in `code_embeddings`. The embed backend (`embed.krolik.tools`) was saturated and timing-out during this feature's development, and the `code_embeddings` table was wiped. Indexing go-code is the highest-value validation because reinvention of helpers (e.g. multiple `pgError` extractors, multiple `retry` patterns) is the exact target class.

**Operator action after embed-server recovers:** run `find_duplicates repo=go-code` to complete the dogfood validation. No code changes needed.

### (b) 5000-function size guard

`semhealthMaxFuncs=5000` means the tool cannot run on larger repos (e.g. `code_cfdb6dd0` with 4426 funcs completes just under the guard; `code_d6e5a5dd` with 5066 funcs is blocked). The four observed target repos (the 2085-func `code_f40acc09` memdb-go, 2085 funcs) **timed out the 15 s statement_timeout** even though they are under the guard — the O(n²) pgvector self-join is slow on the 4-core ARM box under load.

**Root cause:** `FindSimilarPairs` uses a full cross-join `FROM code_embeddings a, code_embeddings b WHERE …` without a HNSW k-NN index anchor. The plan's Phase 5 (per-symbol LATERAL k-NN query) would eliminate this problem by doing n × k-NN lookups instead of one O(n²) join. This is the **top future-work item**.

**Current effective range:** repos with ≤ ~500 functions run reliably within 15 s on the ARM box under typical load. Repos 500–5000 functions may time out at the DB layer and return 0 candidates (silent, not an error from the tool's perspective). The validated repos (234 and 300 funcs) are well within range.

### (c) `filterKind` is currently inert on real data

The embed pipeline only indexes `function` and `method` kinds. The `field`, `var`, `const`, `import` kinds that `filterKind` guards against are never present in `code_embeddings`. Drop count is 0 on all observed runs. This is expected and documented — the filter is forward-defensive for future parser improvements that may emit other kinds.

### (d) Transitive same-file members in merged groups

Documented in §3 above. Not a bug — the filter operates on pairs; union-find operates on the filtered set. A post-processing step that drops group members leaving the group with only same-file companions would improve output quality but is not critical.

---

## 5. Ship decision

**Recommendation: SHIP behind existing DB gating, with one caveat.**

**Evidence supporting ship:**
- Filter invariants hold on two live repos with AGE graphs (CALLS and interface-sibling both verified).
- Top-10 precision sample shows ≥ 70-80% real duplicates at the `very-close` tier — the tier where the tool is most actionable.
- The `calls_edge` filter demonstrably works: 2 pairs were removed from `code_87ce8eca` that would otherwise have been false positives.
- Normal `go test ./internal/semhealth/...` is unaffected by the integration file (build tag isolates it).
- Both surfaces (`code_health` and `find_duplicates` tool) are gated on DB availability — if `DATABASE_URL` is unset, both surfaces gracefully degrade.

**One caveat (operator step, not a blocking concern):**
The go-code dogfood run is DEFERRED. Once `embed.krolik.tools` recovers and go-code is re-indexed, run `find_duplicates repo=go-code` to confirm the tool finds the known reinvented helpers (multiple retry patterns, multiple pgError extractors, etc.). If precision is unacceptable on go-code, revisit the `related` tier threshold or add a go-code-specific IMPLEMENTS edge filter.

**What would change the recommendation to SHELVE:**
- If the go-code dogfood run shows > 50% false positives in the `related` tier AND the `very-close` tier also degrades — indicates the embedding model is not giving clean similarity signal for this codebase.
- If the statement_timeout fires on every repo ≥ 500 funcs after the ARM box load normalizes — indicates Phase 5 LATERAL k-NN is a prerequisite, not nice-to-have.

**Future work (top priority):**
1. Phase 5: replace `FindSimilarPairs` cross-join with per-symbol LATERAL k-NN (removes O(n²) constraint, enables repos up to 50k+ funcs).
2. Post-processing: after union-find, remove group members that are same-file as another member AND have no cross-file pair in the filtered set (cleans up transitive same-file artifacts).
3. IMPLEMENTS edge indexing for TypeScript/Python — would let `interface_sibling` filter remove protocol-pattern false positives (the `close × 4` group in `code_bb3c1bea`).
