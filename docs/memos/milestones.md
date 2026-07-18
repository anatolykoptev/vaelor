# Vaelor Milestones

Performance and capability milestones tracked empirically on a dedicated ARM server.

## code_graph Build Performance

**Repo:** memdb (`/host/src/MemDB`) — 950 files, ~8700 vertices, ~33000 edges, Python+Go

| Milestone | Date | Total | Vertices | Edges | Notes |
|---|---|---|---|---|---|
| Baseline (sequential 1/query) | 2026-04-24 | ~6 min | ~42s | ~165s | code_graph was broken (no AGE extension) |
| AGE fixed + background goroutine | 2026-04-24 | 3m15s | ~161s | ~24s | UNWIND large batches (crashed PG at 200) |
| GIN indexes before inserts + adaptive batch | 2026-04-24 | 1m28s | 38s | 41s | Statement timeout fixed, GIN eliminates O(N²) |
| **Direct COPY INSERT (BulkCopyInsert)** | **2026-04-24** | **14s** | **908ms** | **incl.** | **Bypass Cypher layer, ~100x insert speedup** |

## code_graph Query Latency (cached)

**Repo:** memdb

| Milestone | Date | Latency | Notes |
|---|---|---|---|
| After first successful build | 2026-04-24 | 2.35s | Cypher template query, LLM narrative |

## Supported Languages

| Language | Added | Parser | Type-aware |
|---|---|---|---|
| Go | v1.0 (2026-02-28) | tree-sitter | ✅ go/types |
| Python | v1.0 | tree-sitter | ❌ |
| TypeScript/JS | v1.0 | tree-sitter | ❌ |
| Rust | v1.1 | tree-sitter | ❌ |
| Java | v1.1 | tree-sitter | ❌ |
| C / C++ | v1.1 | tree-sitter | ❌ |
| Ruby | v1.1 | tree-sitter | ❌ |
| C# | v1.1 | tree-sitter | ❌ |
| PHP | v1.16 | tree-sitter | ❌ |

## MCP Tools Count

| Version | Date | Tools | Notable additions |
|---|---|---|---|
| v1.0 | 2026-02-28 | 5 | repo_analyze, file_parse, symbol_search, dep_graph, code_compare |
| v1.6 | 2026-03-xx | 8 | call_trace |
| v1.8 | 2026-03-xx | 10 | code_graph (AGE), code_search |
| v1.13 | 2026-03-xx | 14 | explore, dead_code |
| v1.14 | 2026-03-xx | 16 | code_health, dataflow_analyze |
| v1.18 | 2026-03-16 | 18 | understand, prepare_change (compound tools) |

## Infrastructure

| Component | Status | Notes |
|---|---|---|
| PostgreSQL + AGE | ✅ | postgres-age:17, AGE 1.7.0 |
| AGE UNWIND inserts | ✅ | Stable to 5000+ vertices, adaptive batch sizing |
| AGE direct COPY INSERT | ✅ | v1.20 — bypass Cypher, **14s total build** (was ~6 min) |
| Qdrant (vector search) | ✅ | Used for semantic_search |
| BM25F + PageRank | ✅ | Used for code_research |
| go/types (Go type analysis) | ✅ | v1.18 |
| CE reranker (gte-multi-rerank) | ✅ | v1.21 — reranks semantic_search + dead_code; sigmoid [0..1] scores; embed-server:8082 |
| code_graph query timeout fix | ✅ | addNarrative cap 50 rows; dead_code LIMIT 100 — 2026-04-24 |

## AGE Graph Stats (memdb, 2026-04-24)

```
graph:   code_f40acc09
files:   950
vertices: 8706
  Package: ~626
  File:    950
  Symbol:  ~7120
  Layer:   ~5
  Route:   ~5
edges:   33013
  CALLS:   ~14000
  CONTAINS: ~7950
  IMPORTS:  ~4750
  BELONGS_TO: ~3800
  USES:    ~1400
  others:  ~113
```

## Search Quality Improvements (2026-04-24/25)

| Metric | Before | After | Method |
|---|---|---|---|
| Dead code false positives | All `main()` entrypoints in top-10 | gRPC handlers, complex orphans first | CE reranker + CASE WHEN pre-filter |
| semantic_search "LLM client init" | All `__init__` methods | `NewLLMExtractorWithClient` #1 | pg_trgm finds by name, CE reranks |
| semantic_search timeout | Pool exhaustion during graph build | Never | MaxConns 4->10 + 5s safety timeout |
| code_health large repos | 60s timeout | 1.9ms cached | Background computation + PostgreSQL cache |
| CE dead_code_score UX | -1.7486 (confusing) | 0.148 (probability) | Sigmoid normalization |

## Three-Layer Search Architecture

```
Query -> Vector (semantic) -> top-N candidates
      -> pg_trgm (symbol names) -> code abbreviation matches  
      -> CE reranker (gte-multi-rerank) -> final ordering by relevance
```

Applied to: `semantic_search`, `code_research`, `repo_analyze`

## Infrastructure Reliability

| Component | Status | Notes |
|---|---|---|
| code_graph build (MemDB) | OK 14s | BulkCopyInsert direct COPY, was 6 min |
| semantic_search concurrency | Fixed | pgxpool MaxConns=10, 5s safety timeout |
| code_health (313 deps) | Fixed | Freshness 20-concurrent x 2s, 35s cap |
| code_health cache | Active | 1h TTL, background computation |
