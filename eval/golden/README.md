# Golden dataset for Vaelor retrieval evaluation

Each `<repo>.jsonl` file is a per-repo collection of labeled queries used by
`cmd/eval` to measure semantic_search retrieval quality (nDCG@10, Recall@10/@20,
MRR). The basename (without `.jsonl`) is the value passed to `semantic_search`'s
`repo` argument unless a record overrides `repo`.

## Record schema

One JSON object per line. Lines that begin with `#` and blank lines are
ignored. Schema:

| Field            | Type     | Required | Notes                                                                                       |
|------------------|----------|----------|---------------------------------------------------------------------------------------------|
| `query`          | string   | yes      | Free-form natural-language or identifier query, exactly as a real caller would phrase it.   |
| `expected_top_3` | string[] | yes      | 3 labels (1 minimum) that the harness will count as relevant when present in the top-K.     |
| `repo`           | string   | no       | Override of the `semantic_search` `repo` argument. Defaults to the file basename.           |
| `language`       | string   | no       | Language filter passed to `semantic_search` (e.g. `go`, `python`, `typescript`). When empty, no filter is sent and the record aggregates under the `unspecified` bucket in per-language reports. Backward-compatible: old records without this field parse and run identically (no filter, same search results). |
| `notes`          | string   | no       | Free-form for the labeler. Not used in scoring.                                             |

### Per-language breakdown

When the `language` field is present, the harness:

1. Passes it as the `language` filter to `semantic_search` (narrowing results
   to that language).
2. Aggregates **every** metric (nDCG@10, Recall@10/@20, MRR, latency p50/p95/
   p99/mean) both overall AND per-language in the report's `per_language`
   array.

Records without `language` aggregate under `"unspecified"`. This makes a
feature that helps Go but regresses Python visible in a single run — the
per-language table shows the split.

## Repo path resolution (`--repo-map`)

The golden JSONL files use placeholder repo paths (e.g. `/path/to/repo`) so
the dataset stays portable — no operator-specific paths are committed. At run
time, resolve them with `--repo-map` (or the `REPO_MAP` env var):

```bash
go-code-eval --golden-dir eval/golden \
             --target-url http://127.0.0.1:8897 \
             --repo-map go-code=/host/src/go-code,MemDB=/host/src/MemDB \
             --output /tmp/eval.json
```

The format is comma-separated `repo_key=path` pairs, where `repo_key` is the
`.jsonl` file basename (e.g. `go-code` for `go-code.jsonl`) and `path` is the
real absolute path or forge slug to pass to `semantic_search`. Records whose
`repo_key` is not in the map fall back to the record's own `repo` field.

### `expected_top_3` matching

Labels are matched leniently in three forms — pick whichever is least painful
to write:

1. **Exact `<file>:<symbol>`** — e.g. `internal/embeddings/rrf.go:MergeRRF`.
2. **Symbol only** — e.g. `MergeRRF`. Matches any retrieved hit whose symbol
   name equals the label.
3. **Suffix `<file>:<symbol>`** — e.g. `rrf.go:MergeRRF`. The file portion is
   matched as a path suffix, so labelers don't have to know the full path.

Form 2 is the easiest but the loosest: if multiple repos share a symbol name,
collisions can falsely inflate scores. Use form 1 or 3 for ambiguous symbols.

## Labeling procedure

For each new query you want to add:

1. **Pick the query** — sample from production traces (`gocode_semantic_search_total`
   in Prometheus) or compose a representative one. Aim for a 50/50 split between
   identifier-style queries (`MergeRRF`, `prioritizeFilesWithScores`) and
   natural-language queries ("function that validates JWT tokens").
2. **Find the truth** — run `mcp__go-code__understand` or `mcp__go-code__symbol_search`
   against the live MCP and record the top 1-3 symbols a senior engineer would
   call relevant. The harness uses **binary** relevance, so don't rank within
   the labeled set — anything in `expected_top_3` is treated equally.
3. **Append a JSONL line** to the appropriate `<repo>.jsonl`. Include `notes`
   when the query is non-obvious or when you skipped a near-miss.

Target corpus size: 60-80 records total across the shipped repos (go-code,
MemDB). Skewing toward the bigger repo (MemDB) is fine — it covers more
retrieval patterns. (Additional local-only targets can be added as
`eval/golden/<repo>.jsonl`; the harness globs the directory.)

## Coverage targets

| Repo           | Records | Why                                                                |
|----------------|---------|--------------------------------------------------------------------|
| `go-code`      | 30-40   | Self-test; many small files exercise exact-symbol matching.        |
| `MemDB`        | 30-40   | Go + pgvector, dense per-symbol descriptions.                      |
| `psf-requests` | 50      | Python pilot — real OSS repo, NL + identifier + concept queries.   |

## Python golden set (`psf-requests.jsonl`)

- **Repo**: `psf/requests` (https://github.com/psf/requests)
- **Pinned SHA**: `6e83187b8feb273ed4c6cdab5efd8d54901dfab3` (tag v2.34.2)
- **Records**: 50 (20 natural-language, 15 identifier, 10 concept/behavior,
  5 synonym/paraphrase)
- **Language**: `python` (set on every record via the `language` field)
- **Labeling method**: every `expected_top_3` entry was verified by running
  `mcp__vaelor__symbol_search` and `mcp__vaelor__code_search` against the
  live vaelor MCP, which cloned `psf/requests` at HEAD (source files in
  `src/requests/` are identical between the pinned tag and HEAD — only CI
  config bumps differ). Each label is a real symbol path confirmed by
  AST-level symbol_search results (file + line + kind). ZERO guessed or
  unverified labels. Queries with no clear verifiable ground truth were
  dropped rather than stubbed.
- **Resolve at run time**: `--repo-map psf-requests=/host/src/psf-requests`
  (or use the forge slug `psf/requests` as the `repo` override).

## Reproducibility

Within one run, the harness dispatches queries concurrently but writes results
in deterministic order (per-repo alphabetic, then per-query alphabetic). Two
runs against the same target server should produce numerically identical
metrics modulo embed-server jitter (cosine distance to 4 decimal places).
