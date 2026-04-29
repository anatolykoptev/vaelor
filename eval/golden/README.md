# Golden dataset for go-code retrieval evaluation

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
| `notes`          | string   | no       | Free-form for the labeler. Not used in scoring.                                             |

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

Target corpus size: 100-200 records total across the 5 repos. Skewing toward
the bigger repos (vaelor, MemDB) is fine — they cover more retrieval patterns.

## Coverage targets

| Repo           | Records | Why                                                                |
|----------------|---------|--------------------------------------------------------------------|
| `go-code`      | 30-40   | Self-test; many small files exercise exact-symbol matching.        |
| `MemDB`        | 30-40   | Go + pgvector, dense per-symbol descriptions.                      |
| `vaelor`       | 30-40   | Largest Go corpus, exercises scale-drift.                          |
| `acme-web` | 20-30   | Rust path; tests cross-language tree-sitter parsing.               |
| `acme-guide`    | 15-25   | Polyglot (Astro + WP); tests language-mixed retrieval.             |

## Reproducibility

Within one run, the harness dispatches queries concurrently but writes results
in deterministic order (per-repo alphabetic, then per-query alphabetic). Two
runs against the same target server should produce numerically identical
metrics modulo embed-server jitter (cosine distance to 4 decimal places).
