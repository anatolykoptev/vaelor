# go-code retrieval-quality evaluation harness

Offline harness that replays a labeled golden dataset against a running
go-code MCP server and reports retrieval-quality metrics with optional
A/B significance testing.

## What it measures

For each labeled `(query, expected_top_3)` pair the harness calls
`semantic_search(repo, query, top_k=20)` and computes:

| Metric      | Definition                                                                           |
|-------------|--------------------------------------------------------------------------------------|
| nDCG@10     | DCG@10 / IDCG@10. Binary relevance: hit at rank `i` scores `1/log2(i+1)` if relevant.|
| Recall@10   | `\|expected ∩ retrieved_top_10\| / \|expected\|`.                                    |
| Recall@20   | Same with top-20.                                                                    |
| MRR         | `1 / rank_of_first_relevant_hit`, or `0` when no relevant hit appears.               |

Metrics are reported per-query, per-repo (mean), and overall (mean of all
queries). With `--baseline`, a paired Student's t-test on per-query scores
also reports two-tailed p-values in the JSON `delta` block.

## Build

```
CGO_ENABLED=1 GOWORK=off go build -o /tmp/go-code-eval ./cmd/eval/
```

The binary is self-contained — no runtime deps besides a reachable go-code
MCP server with the REST bridge enabled (default in `cmd/go-code/main.go`).

## End-to-end run

```
# 1. Capture baseline against production go-code BEFORE touching streams 1-3.
/tmp/go-code-eval \
  --golden-dir eval/golden \
  --target-url http://127.0.0.1:8897 \
  --output     /tmp/eval-baseline.json

# 2. Land the candidate change (e.g. weighted RRF), restart go-code,
#    then re-run the harness against the same target.
/tmp/go-code-eval \
  --golden-dir eval/golden \
  --target-url http://127.0.0.1:8897 \
  --output     /tmp/eval-candidate.json \
  --baseline   /tmp/eval-baseline.json
```

The candidate report's `delta` block reports per-metric mean change and a
paired t-test p-value against baseline. Sprint streams 1-3 ship only when
the relevant metric reports `p < 0.05`.

## Flags

| Flag           | Default                  | Notes                                                |
|----------------|--------------------------|------------------------------------------------------|
| `--golden-dir`    | `eval/golden`            | Directory of `<repo>.jsonl` files.                         |
| `--target-url`    | `http://127.0.0.1:8897`  | MCP base URL; harness calls `/api/tools/...` on it.        |
| `--output`        | stdout                   | JSON report path. `-` writes to stdout.                    |
| `--baseline`      | (none)                   | Prior report path; enables the `delta` block.              |
| `--splade-weight` | (none)                   | RRF_WEIGHT_SPARSE value used in candidate run. Enables the `splade_gate` verdict. Requires `--baseline`. |
| `--workers`       | `8`                      | Concurrent HTTP workers.                                   |
| `--top-k`         | `20`                     | `top_k` passed to `semantic_search`. Min 20.               |
| `--timeout`       | `30m`                    | Overall harness timeout.                                   |

## Output schema

```jsonc
{
  "metadata": {
    "timestamp": "2026-04-29T12:00:00Z",
    "target_url": "http://127.0.0.1:8897",
    "git_sha": "deadbeef…",
    "golden_dir": "eval/golden",
    "top_k": 20
  },
  "per_query": [
    {
      "repo": "go-code",
      "query": "merge rrf",
      "expected_top_3": ["MergeRRF"],
      "retrieved_top_20": ["internal/embeddings/rrf.go:MergeRRF", …],
      "ndcg10": 1.0, "recall10": 1.0, "recall20": 1.0, "mrr": 1.0
    }
  ],
  "per_repo": [
    {"repo": "go-code", "ndcg10": 0.84, "recall10": 0.78, "recall20": 0.83, "mrr": 0.91, "queries": 35, "errors": 0}
  ],
  "aggregates": {"ndcg10": 0.78, "recall10": 0.62, "recall20": 0.71, "mrr": 0.81, "queries": 165, "errors": 3},
  "delta": {                              // omitted when --baseline is unset
    "ndcg10":  "+0.0340 (p=0.0120)",
    "recall10": "+0.0280 (p=0.0450)",
    "recall20": "+0.0190 (p=0.0810)",
    "mrr":      "+0.0510 (p=0.0080)"
  },
  "splade_gate": {                        // only set when --splade-weight is given with --baseline
    "verdict": "PASS",                    // PASS | FAIL | INSUFFICIENT_DATA
    "tested_weight": 0.3,
    "recommended_action": "Set RRF_WEIGHT_SPARSE=0.30 in production. …",
    "ndcg10_delta": 0.034,
    "ndcg10_p": 0.012,
    "recall20_delta": 0.019,
    "recall20_p": 0.081,
    "paired_queries": 49,
    "ndcg10_significant": true,
    "recall20_non_inferior": true,
    "explanation": "nDCG@10 improved by +0.0340 (p=0.0120 < 0.05) …"
  }
}
```

## SPLADE A/B gate (Phase P6)

Use the harness to decide whether to flip `RRF_WEIGHT_SPARSE` from 0.

### Prerequisites

1. **P5 backfill complete**: `sparse_embedding` column must be populated
   for the target repos (check `gocode_sparse_backfill_remaining` gauge in
   Prometheus — should be 0 or near 0).
2. **`SPARSE_EMBED_URL` set** on the go-code server: enables sparse embed
   and retrieval. Without it the sparse arm is a no-op.

### Procedure

```bash
# Step 1: baseline run — RRF_WEIGHT_SPARSE=0 (sparse arm inert)
# Set RRF_WEIGHT_SPARSE=0 in go-code environment, restart, then:
/tmp/go-code-eval \
  --golden-dir eval/golden \
  --target-url http://127.0.0.1:8897 \
  --output /tmp/eval-baseline.json

# Step 2: candidate run — set RRF_WEIGHT_SPARSE=0.3, restart go-code, then:
/tmp/go-code-eval \
  --golden-dir eval/golden \
  --target-url http://127.0.0.1:8897 \
  --output /tmp/eval-cand.json \
  --baseline /tmp/eval-baseline.json \
  --splade-weight 0.3

# Inspect the verdict:
jq .splade_gate /tmp/eval-cand.json
```

Sweep multiple weights (0.2, 0.3, 0.4 per the research-recommended range)
and pick the weight with the best `ndcg10_delta` and a PASS verdict.

### Gate rule

**PASS** (flip `RRF_WEIGHT_SPARSE` to tested weight) when ALL of:
- nDCG@10 delta > 0 with p < 0.05 (paired t-test, two-tailed)
- Recall@20 delta ≥ −2% OR Recall@20 p ≥ 0.05 (non-inferior)

**FAIL** otherwise — do not flip the weight. Leave at 0 and revisit.

### Future step: Team Draft Interleaving (TDI)

TDI interleaves two ranking systems on live traffic and measures
click-through preference with higher sensitivity than offline nDCG.
After this offline PASS, TDI is the recommended online validation
before making `RRF_WEIGHT_SPARSE > 0` the permanent default.
TDI requires live traffic instrumentation and is out of scope for P6.

## Recommended sprint workflow

1. **Pre-merge baseline**: snapshot the harness output against the current
   `main`. Commit the JSON alongside your planning notes
   so reviewers can audit the baseline.
2. **Per-stream candidate**: after each stream lands on its feature branch,
   point the harness at a build of that branch and capture
   `/tmp/eval-<stream>.json` with `--baseline` set to step 1's snapshot.
3. **Decide ship**: streams 1-3 ship when `delta.<metric>` reports p < 0.05
   for the metric they target, per the sprint plan's success criteria.
4. **Post-merge re-baseline**: after a stream merges to `main`, capture a new
   baseline so the next stream's candidate is paired against the latest state.

## Labeling

See [`golden/README.md`](golden/README.md) for record schema, lenient label
matching rules, and the 3-step labeling procedure.

## Scope and limitations

- The harness queries the **live** target, so embed-server jitter affects the
  per-query distance values (not the rank order). Bound this by re-running the
  harness twice; the t-test absorbs small noise.
- Relevance is **binary**. A query whose label contains a 4th, 5th, … relevant
  symbol is undercounted by Recall@10/@20. Add the missing symbols to the label
  rather than tuning the metric.
- The harness does **not** drive autoindex — make sure the target has indexed
  the 5 repos before the first run, otherwise the first pass will return
  `<status>indexing</status>` envelopes that the harness counts as zero hits.
