# Code Quality & Metrics Tools

## panbanda/omen — Hotspot Detection

- **Repo**: [panbanda/omen](https://github.com/panbanda/omen) | Rust
- **Key file**: `src/analyzers/hotspot.rs`

### Formula
```
hotspot = percentile(churn) × percentile(complexity)
```

Product of percentile ranks, not sum.

### Thresholds
- Critical: ≥ 0.81
- High: ≥ 0.64
- Moderate: ≥ 0.36
- Both churn and complexity must be ≥ 50th percentile

### Churn Scoring
`sum(1.0 + (additions + deletions) / 100.0)` per commit — larger changes weighted more.

**Status in Vaelor**: Planned for Phase 9.1 (complexity metrics + hotspot detection).

## boyter/scc — Code Counting

- **Repo**: [boyter/scc](https://github.com/boyter/scc) | 8,071 stars | Go
- LOC, blank, comment, cyclomatic complexity per file. Duplicate detection.
- Useful for quantitative repo fingerprinting in `code_compare`.

## gabotechs/dep-tree — Dependency Graph Visualization

- **Repo**: [gabotechs/dep-tree](https://github.com/gabotechs/dep-tree) | 1,696 stars | Go
- Cross-language dependency graph visualization.
- Reference for dep graph rendering.

## Neural Code Retrieval (arxiv 2502.07067)

- **Title**: "Repository-level Code Search with Neural Retrieval Methods" (Feb 2025)
- **Pipeline**: BM25 over commit messages → CodeBERT CommitReranker → CodeBERT CodeReranker

### Key Insight
Commit messages are natural language descriptions of what code does.
BM25 on commit messages **outperforms** BM25 on source code for bug localization.

- **Result**: up to 80% improvement in MAP/MRR/P@1 vs BM25 baseline

## Academic Research

| Paper | Key Finding |
|-------|-------------|
| [CodeCompass (arxiv 2602.20048)](https://arxiv.org/abs/2602.20048) | Graph-based navigation: 99.4% task completion vs 76.2% baseline. But 58% of agents with graph access made 0 tool calls — need explicit prompting. |
| [MLSA (arxiv 1808.01213)](https://arxiv.org/abs/1808.01213) | Build monolingual call graphs independently, stitch at FFI boundaries. |
| [CHARON (EuroSP 2025)](https://scnps.co/papers/eurosp25_polyglot_sast.pdf) | Polyglot Property Graphs with bidirectional cross-language edges for SAST. |

## CE Cross-Encoder Dead Code Detection (Vaelor approach)

**Problem with AST-only approaches**: Cyclomatic complexity detects complex functions but not unused ones.
Static "zero callers" detection has high false positive rate (entrypoints, test utilities, generated code).

**Vaelor approach (2026-04-24)**:
1. Cypher query finds orphan functions (no CALLS edges in AGE graph)
2. CASE WHEN pre-filter scores by signal: penalizes `main`/`Test*`/`evaluation/` paths, boosts `src/` + high complexity
3. CE reranker (gte-multi-rerank) scores each candidate as (query, function_signature+file) pair
4. Sigmoid normalization: raw logit -> probability [0..1]
5. Results stored in `code_dead_code_scores` at build time, served instantly on query

**Quality**: CE sees `_init_chat_llms` as lower dead-code probability (it initializes components) vs `calculate_scores` (complex function, no callers). Pure complexity-based approaches cannot make this distinction.

**Performance**: 119 candidates -> CE reranker in ~35s at graph build (background) -> zero cost per query.
