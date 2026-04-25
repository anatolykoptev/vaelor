# MILESTONES.md

> Ключевые достижения go-code — измеренные эмпирически на example (ARM 24GB, Oracle Cloud, San Jose CA).
> Все цифры получены на реальных данных, не синтетических тестах.

---

## ЧТО ТАКОЕ go-code

**go-code** — MCP-сервер для глубокой работы с кодом. Понимает репозитории так, как их понимает senior engineer: знает call graph, видит dead code, умеет искать по смыслу, оценивает качество с конкретными рекомендациями.

**Развёрнут:** `http://your-host.example.com/code/mcp` (example ARM, порт 8897)  
**Индексирует:** 45 репозиториев в фоне, включая go-code, MemDB, go-wowa, piter-server, ox-billing

---

## ПРОИЗВОДИТЕЛЬНОСТЬ: code_graph Build

**Репозиторий:** memdb — 955 файлов, 8700+ вершин, 33000+ рёбер, Python + Go

| Этап | Дата | Итого | Вставка вершин | Вставка рёбер | Что изменилось |
|---|---|---|---|---|---|
| Baseline (сломано) | 2026-04-24 | ∞ | — | — | AGE extension не создан, `CREATE EXTENSION age` отсутствовал |
| AGE починен, sequential | 2026-04-24 | ~6 мин | ~42s | ~165s | Каждая вершина — отдельный Cypher запрос |
| UNWIND batching | 2026-04-24 | 3m15s | 161s (медленнее!) | 24s | AGE крашит PG при N>20, UNWIND парсится медленно для сложных символов |
| GIN indexes before inserts | 2026-04-24 | 1m28s | 38s | 41s | Устранён O(N²) MERGE scan, переехали EnsureIndexes |
| **BulkCopyInsert** | **2026-04-24** | **14s** | **908ms** | **incl.** | **Прямой COPY INTO AGE таблицы, обход Cypher parser** |

**Итоговое ускорение: 26x** (6 мин → 14 секунд)

### Ключевые открытия в процессе

- AGE крашит PG при multi-MERGE batch >20 вершин (ARM-специфика)
- UNWIND медленнее sequential для Symbol (150KB запрос, AGE парсит 9s)
- GIN index перед вставкой устраняет O(N²): без него MERGE делает full scan растущей таблицы
- **BulkCopyInsert**: `graphid = (label_id << 48) | seq`, agtype принимает JSON строку через text-format COPY — прямая вставка в PostgreSQL, Cypher parser вообще не участвует

---

## ПРОИЗВОДИТЕЛЬНОСТЬ: code_graph Queries (cached)

| Запрос | Latency | Notes |
|---|---|---|
| dead_code (pre-computed) | <1ms PG lookup | CE scores вычислены при build |
| code_graph Cypher template | 2.35s | AGE query + LLM narrative |
| code_graph custom query | 3-8s | NL→Cypher generation + execution |

---

## ТРЁХСЛОЙНЫЙ ПОИСК

### Архитектура

```
Запрос
  │
  ├─ Layer 1: Vector embeddings (jina-code-v2)
  │   Находит семантически похожий код
  │   Проблема: "initializes LLM client" → все __init__ методы (слепой к именам)
  │
  ├─ Layer 2: pg_trgm trigram similarity
  │   Ищет по именам символов в embedding table
  │   Решение: "init" + "llm" → находит NewLLMExtractorWithClient по имени
  │   Технология: similarity(symbol_name, query_keywords) ORDER BY score DESC
  │
  └─ Layer 3: CE Cross-Encoder (gte-multi-rerank, 306M params)
      Видит (query, code_signature) одновременно
      Финальный порядок: NewLLMExtractorWithClient #1 vs __init__ в llms/base.py #2
```

### Применение

| Инструмент | Layer 1 | Layer 2 | Layer 3 |
|---|---|---|---|
| `semantic_search` | ✅ jina-code-v2 | ✅ pg_trgm | ✅ CE reranker |
| `code_research` | ✅ embeddings | ✅ pg_trgm seeds | ✅ CE rerank symbols |
| `repo_analyze` | ✅ BM25F+PageRank | ✅ +0.3 boost by name | — |

### Измеренное улучшение

| Запрос | До (только вектор) | После (3 слоя) |
|---|---|---|
| "initializes LLM client" | #1-8: все `__init__` методы | #1: `NewLLMExtractorWithClient` (Go) |
| "memory search retrieval" | generic `__init__` | `_extract_embeddings`, `search_memory` |
| "LLM dead code" | случайный порядок | `calculate_scores` (complexity=64) выше |

---

## DEAD CODE: CE CONFIDENCE SCORES

### Pipeline

```
1. Cypher: MATCH orphan functions (no CALLS edges)
   ORDER BY CASE WHEN pre-score (complexity × path_penalty × name_penalty) DESC
   LIMIT ~120 (= symbolCount/60, capped [50..200])

2. CE reranker query: "orphaned function with no callers that is a bug risk"
   Documents: "name: func_name, file: path, complexity: N, code: def func_name(...):"

3. Sigmoid normalization: probability = 1/(1+exp(-raw_logit))
   Score -0.5 → 0.38 (moderate dead code), -1.75 → 0.15 (unlikely dead code)

4. Persist to code_dead_code_scores (repo_key, name, file, score)
   TTL: rebuilt each IndexRepo (~1h)
```

### Где используется

- `code_graph` dead_code template → pre-computed scores, instant lookup
- `understand FunctionX` → показывает `dead_code_score: 0.148`
- `dead_code` MCP tool → `ceScore` атрибут на каждом символе
- `review_delta` → `deadCodeNote` на удалённых символах
- `code_health` → `deadCodeCandidates` в metrics, -0.4/кандидат в score

### Качество

**До CE:** topicality в dead_code запросах — все `main()` из evaluation/ скриптов, ноль production кода

**После CE + smart pre-filter:**
- #1 `NewChatServiceClient` (gRPC generated, реально не вызывается внутри)
- #2 `NewMemoryServiceHandler`
- #3 `calculate_scores` (complexity=64, evaluation скрипт)
- `main()` entrypoints → правильно на #18-19

---

## code_health: GRADE A-F

### Что измеряет

| Метрика | Вес | Source |
|---|---|---|
| Cyclomatic complexity (avg + max) | высокий | tree-sitter AST |
| Cognitive complexity | высокий | tree-sitter AST |
| Test coverage ratio | высокий | file naming patterns |
| Documentation ratio | средний | doc comments |
| Error handling ratio | средний | AST patterns |
| Duplication | средний | hash-based |
| Magic numbers | низкий | literal detection |
| Dep freshness | средний | PyPI/npm HTTP |
| Vulnerability security | средний | OSV API |
| **Dead code candidates** | **новый** | **CE scores** |
| Semantic duplicates | средний | embeddings |

### Large repo solution

```
Первый вызов:
  → check code_health_cache (PostgreSQL, 1h TTL)
  → cache miss → start background goroutine (5 min budget)
  → return "computing, retry in 60s"

Background goroutine:
  → buildHealthSnapshot (AST parse, ~10s)
  → parallel: freshness (35s cap) + hotspots + arch metrics + dead code
  → persist result_xml to code_health_cache

Повторный вызов:
  → cache hit → return 1.9ms ← из PostgreSQL
```

**Проблема freshness до фикса:** 313 PyPI deps × 3s timeout / 10 concurrent = 94s worst case  
**После фикса:** 20 concurrent × 2s × 35s cap = max 35s

---

## INFRASTRUCTURE

| Компонент | Status | Версия/Config | Notes |
|---|---|---|---|
| PostgreSQL + AGE | ✅ | example-postgres-age:17, AGE 1.7.0 | custom ARM Docker image |
| BulkCopyInsert | ✅ | — | text-format COPY, graphid formula |
| code_graph cache | ✅ | TTL=3600s | `code_graph_meta` PG table |
| code_health cache | ✅ | TTL=3600s | `code_health_cache` PG table |
| dead_code_scores | ✅ | — | `code_dead_code_scores` PG table |
| Vector embeddings | ✅ | jina-code-v2 (768-dim), multilingual-e5-large | embed-server:8082 (Rust + ONNX) |
| CE reranker | ✅ | gte-multi-rerank (306M), max_len=256, INT8 | embed-server:8082 |
| pg_trgm symbol search | ✅ | similarity() > 0.1 | `code_embeddings` table |
| pgxpool | ✅ | MaxConns=10 | был 4, exhaustion при concurrent build |
| Background builds | ✅ | sync.Map buildingRepos | code_graph + code_health |
| Autoindex | ✅ | 45 repos | embeddings при каждом старте |

---

## ИНСТРУМЕНТЫ (18 MCP tools)

| Инструмент | Назначение | Использует граф |
|---|---|---|
| `repo_analyze` | Full AST analysis + LLM summary | BM25F+PageRank+pg_trgm |
| `code_graph` | AGE Cypher queries, NL→Cypher | ✅ AGE (CALLS/IMPORTS/etc) |
| `semantic_search` | Natural language → code | ✅ vectors+pg_trgm+CE |
| `code_research` | Aider-style context map | ✅ vectors+pg_trgm+CE |
| `understand` | Deep symbol analysis | ✅ pagerank+community+dead_code |
| `dead_code` | AST-based dead code + CE scores | ✅ code_dead_code_scores |
| `impact_analysis` | Blast radius of changes | call graph |
| `call_trace` | Execution path tracing | ✅ CrossRefs (TestedBy, routes) |
| `code_health` | Grade A-F, recommendations | ✅ arch metrics + dead code |
| `code_compare` | Two repos structural diff | — |
| `review_pr` | PR review with graph signals | ✅ community+surprise+dead_code |
| `review_delta` | Branch change analysis | ✅ dead_code annotations |
| `dep_graph` | Import graph visualization | — |
| `file_parse` | Single file AST/symbols | — |
| `symbol_search` | Wildcard pattern search | — |
| `code_search` | Literal/regex in codebase | — |
| `dataflow_analyze` | Taint tracking, dead stores | ox-codes |
| `explore` | Repo overview, no LLM | — |

---

## ЯЗЫКИ

| Язык | Парсер | Type-aware | CE dead code |
|---|---|---|---|
| Go | tree-sitter + go/types | ✅ | ✅ |
| Python | tree-sitter | ❌ | ✅ |
| TypeScript/JS | tree-sitter | ❌ | ✅ |
| Rust | tree-sitter | ❌ | ✅ |
| Java | tree-sitter | ❌ | ✅ |
| C/C++ | tree-sitter | ❌ | ✅ |
| Ruby | tree-sitter | ❌ | ✅ |
| C# | tree-sitter | ❌ | ✅ |
| PHP | tree-sitter | ❌ | ✅ |

---

## СРАВНЕНИЕ С КОНКУРЕНТАМИ

| Возможность | go-code | GitHub Copilot | Cursor | Sourcegraph | SonarQube |
|---|---|---|---|---|---|
| Cross-language call graph | ✅ Python+Go+... | ❌ | ❌ | ✅ (дорого) | ❌ |
| CE dead code probability | ✅ sigmoid [0..1] | ❌ | ❌ | ❌ | ❌ |
| Semantic search + CE reranker | ✅ 3 layers | ❌ | ✅ partial | ✅ | ❌ |
| code_health grade A-F | ✅ incl. dead code | ❌ | ❌ | ❌ | ✅ |
| MCP-native (agents) | ✅ | ✅ | partial | ❌ | ❌ |
| На твоём железе | ✅ ARM example | ❌ SaaS | ❌ SaaS | ❌ SaaS | ✅ |
| Стоимость | self-hosted | $10-19/мес | $20/мес | $50K+ enterprise | $150+/мес |

---

## СЛЕДУЮЩИЕ ШАГИ

| Приоритет | Задача | Ожидаемый impact |
|---|---|---|
| Высокий | `load_labels_from_file` bulk load | code_graph 14s → <5s |
| Высокий | CE reranker для `review_delta` (live) | Лучший PR review |
| Средний | `impact_analysis` + AGE cross-language | Python→Go blast radius |
| Средний | `understand` + community peers | Показ связанных символов |
| Низкий | Меньший CE reranker (22M params) | semantic_search <1s |
