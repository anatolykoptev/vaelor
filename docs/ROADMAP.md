# go-code Implementation Roadmap

## v1.0: Foundation — Parse & Analyze (MVP) ✅

**Goal**: Replace `github_repo_analyze` with a better version.
Single tool (`repo_analyze`) that works better than the current one.

**Status**: Complete (2026-02-28). Deployed on :8897, registered as MCP server.

### 1.1 tree-sitter integration ✅
- [x] Add `smacker/go-tree-sitter` dependency
- [x] Implement `LanguageHandler` interface
- [x] Go handler with `go.scm` queries — functions, methods, types, imports, consts
- [x] Python handler with `python.scm` queries
- [x] TypeScript/JavaScript handler with `typescript.scm` queries
- [x] Unit tests: parse known Go/Python/TS files, verify symbol extraction

### 1.2 Improved ingestion ✅
- [x] Clone, walk, filter with gitignore support
- [x] Fix file loss: configurable limits (10K files, 20 depth), smarter filtering
- [x] File tree rendering (box-drawing, max 100 lines)
- [x] Integration tests: 25 tests covering all ingestion features
- [ ] Git change frequency for relevance ranking (deferred — using symbol count proxy)

### 1.3 Smart cleaning ✅
- [x] Per-language comment stripping (C-style + hash-style)
- [x] Preservation rules: TODO/FIXME/nolint/doc comments kept
- [x] Blank line collapsing, long line truncation, file-level truncation
- [x] 14 tests covering all cleaning modes
- [x] Signatures-only mode (v1.3)
- [x] Skeleton mode with `...` placeholders (v1.3)

### 1.4 LLM analysis ✅
- [x] LLM client via OpenAI-compatible proxy
- [x] System prompts for repo analysis, code comparison, dep graph
- [x] LLM context builder with 150K char budget
- [x] File prioritization by query relevance + import frequency + symbol count
- [x] Multi-level analysis: overview → module → deep (v1.4)
- [ ] JSON output parsing with fallback (deferred)

### 1.5 MCP tools ✅
- [x] `repo_analyze` — ingest → parse → clean → LLM → structured answer
- [x] `file_parse` — tree-sitter AST/symbol extraction for single files
- [x] `symbol_search` — wildcard pattern matching across repos
- [x] `dep_graph` — import graph in mermaid/dot/json formats
- [x] `code_compare` — registered as stub (v1.5)
- [x] Support GitHub repos (clone) and local paths
- [x] Health endpoint (`/health`)
- [x] Docker build and deploy (docker-compose + MCP registration)

**Deliverable**: 5 MCP tools on :8897. `repo_analyze` + `file_parse` + `symbol_search` + `dep_graph` working. ✅

---

## v1.1–v1.4: Structure — Enhanced Parsing & Cleaning ✅

**Goal**: Improve code understanding quality. Additional languages, smarter cleaning modes, caching.

### v1.1: Additional languages ✅

**Status**: Complete (2026-02-28). 6 new languages added, 12 parser tests passing.

- [x] Rust handler (`handler_rust.go` + `rust.scm`) — functions, methods, structs, enums, traits, type aliases, consts, statics
- [x] Java handler (`handler_java.go` + `java.scm`) — classes, interfaces, enums, methods, constructors
- [x] C handler (`handler_c.go` + `c.scm`) — functions, structs, enums, typedefs, macros, globals
- [x] C++ handler (`handler_cpp.go` + `cpp.scm`) — extends C with classes, methods, namespaces, templates, qualified identifiers
- [x] Ruby handler (`handler_ruby.go` + `ruby.scm`) — methods, singleton methods, classes, modules, constants
- [x] C# handler (`handler_csharp.go` + `csharp.scm`) — classes, interfaces, structs, enums, methods, constructors, namespaces

**Total supported languages**: Go, Python, TypeScript/JS, Rust, Java, C, C++, Ruby, C# (9 languages).

### v1.2: Noise reduction & quality fixes ✅

**Status**: Complete (2026-02-28).

- [x] `testdata/` added to default ignore dirs (all tools benefit)
- [x] `ExcludeTests` option — `symbol_search` and `dep_graph` skip `_test.go` files; `repo_analyze` keeps them for full picture
- [x] `symbol_search` result limit (default 100, max 500) — prevents unbounded output
- [x] `dep_graph`: stdlib imports filtered by default (`IncludeStdlib` opt-in)
- [x] `dep_graph`: self-import edges removed (test files importing own package)
- [x] `dep_graph`: format validation — unknown format returns error instead of silent fallback
- [x] Go parser: skip function-local var/const declarations (only package-level symbols)
- [x] Go parser: const block signature shows individual spec, not `const (`

### v1.3: Advanced cleaning modes ✅

**Status**: Complete (2026-02-28). New `internal/render` package, `mode` parameter on `repo_analyze`.

- [x] Signatures-only mode: extract API surface without bodies
- [x] Skeleton mode: structure with `// ...` placeholders
- [x] Focused mode: full bodies for query-relevant symbols, signatures for rest
- [x] Structural kinds (struct/interface/class/type) always preserve full body
- [x] Exposed as `mode` parameter on `repo_analyze` MCP tool

### v1.4: Multi-level analysis + caching ✅

**Status**: Complete (2026-02-28). `depth` parameter on `repo_analyze`, three analysis levels.

- [x] Level 1 (overview): file tree + symbol signatures only (50K budget, signatures render mode)
- [x] Level 2 (module): selected files + dependency graph in Mermaid (150K budget, skeleton render mode)
- [x] Level 3 (deep): full function bodies for relevant symbols (200K budget, focused render mode)
- [x] Depth-aware system prompts (`SystemPromptForDepth`)
- [x] Default render mode inferred from depth (explicit `mode` overrides)
- [x] Dependency graph section injected between symbol summary and file contents
- [x] LLM response cache by FNV-1a hash of (systemPrompt + userPrompt), TTL 1h, LRU eviction (500 entries)
- [x] Parsed AST cache by (filePath, modTime, size) key, LRU eviction (5000 entries)
- [x] Configurable via env: `PARSE_CACHE_SIZE`, `LLM_CACHE_SIZE`, `LLM_CACHE_TTL_MIN`
- [x] 14 unit tests: get/set, TTL expiry, LRU eviction, staleness, concurrent access

**Deliverable**: Better analysis quality, more languages, faster repeated queries. ✅

---

## v1.5: Comparison Engine ✅

**Goal**: Compare implementations between repositories to find the better solution.

**Status**: Complete (2026-02-28). `code_compare` tool fully operational.

### Structural diff ✅
- [x] Symbol-level matching: exact (name+kind), fuzzy (Levenshtein, threshold 0.7), semantic (LLM classifier)
- [x] Side-by-side alignment of matched symbols with match score
- [x] Coverage gap detection (symbols missing from one side)
- [x] Metrics: avg/max function lines, test ratio, doc ratio, error handling ratio, interfaces, external deps (9 signals)

### `code_compare` tool ✅
- [x] Input: 2 repos (GitHub or local) + query + optional focus/language filter
- [x] Parallel snapshot building (goroutine worker pool)
- [x] Three-pass symbol matching (exact → fuzzy → semantic)
- [x] LLM analysis: quality verdicts, coverage gaps, architecture insights, recommendations
- [x] JSON output structured for AI consumption (CompareResult)
- [x] Budget-aware LLM context assembly (180K chars, 3K per snippet, 80 matched pairs, 40 gaps)

### Module-level comparison ✅
- [x] Focus parameter for subdirectory-level comparison
- [x] Language filter for cross-language comparison
- [x] Quality-focused LLM prompt: finds better solution, not just differences

**Deliverable**: `code_compare` tool that compares two implementations and finds the better one. ✅

---

## v1.6: Call Chain Tracing ✅

**Goal**: Trace execution paths through codebases.

**Status**: Complete (2026-02-27). `call_trace` MCP tool operational.

- [x] Call extraction via tree-sitter queries for all 9 languages (separate `*_calls.scm` files)
- [x] Name-based resolution: same-file → same-package → cross-package
- [x] BFS trace with configurable depth (default 5, max 10)
- [x] Bidirectional: callees (forward) and callers (reverse)
- [x] Cycle detection (marks cycles, avoids infinite loops)
- [x] LLM narrative explanation of execution flow
- [x] `call_trace` MCP tool with JSON output

**Deliverable**: `call_trace` tool — bidirectional BFS + LLM narrative. ✅

---

## v1.7: go-search Migration ✅

**Goal**: Remove code tools from go-search, point to go-code.

**Status**: Complete (2026-02-28). All code tools migrated to go-code, removed from go-search.

### New infrastructure ✅
- [x] `internal/retry` — generic exponential backoff with jitter
- [x] `internal/metrics` — atomic operation counters
- [x] `internal/cache` — GenericCache[T] with Redis L2 (go-redis/v9)
- [x] `internal/llm` — retry + fallback API keys + CompleteRaw
- [x] `internal/github` — SearchCode, SearchIssues, SearchRepos, ExtractOwnerRepo
- [x] `internal/search` — SearXNG client with FilterByScore, DedupByDomain

### New tool modes ✅
- [x] `repo_analyze` mode=quick — GitHub Code Search + LLM summary
- [x] `repo_analyze` type=issue/pr — GitHub Issues/PR search + LLM analysis
- [x] `repo_search` — parallel SearXNG + GitHub API search, enrichment, LLM recommendations

### go-search cleanup ✅
- [x] Remove `tool_github_repo_analyze.go` from go-search
- [x] Remove `tool_github_repo_search.go` from go-search
- [x] Remove `internal/gitingest/` from go-search
- [x] Remove code-specific functions from `sources/github.go`
- [x] Update go-search CLAUDE.md, tool count, metrics
- [x] Deploy both services, verify health

**Deliverable**: Clean separation. go-search = web search. go-code = code intelligence. ✅

---

## v1.8: Code Graph ✅

**Goal**: Persistent knowledge graph backed by Apache AGE.

**Status**: Complete (2026-02-28). `code_graph` MCP tool operational.

- [x] Store symbols + relationships in Apache AGE (separate `gocode` database)
- [x] Schema: Package/File/Symbol vertices + CONTAINS/CALLS edges
- [x] 10 Cypher query templates (who_calls, calls_of, imports_of, symbols_in, etc.)
- [x] NL → Cypher hybrid: template classification + freeform LLM generation fallback
- [x] Lazy indexing with TTL cache (1h local, 24h remote)
- [x] Read-only guard on freeform Cypher (blocks writes)
- [x] `code_graph` MCP tool with JSON + LLM narrative output

**Deliverable**: `code_graph` tool — NL→Cypher + LLM narrative. ✅

---

## v1.9: Cross-Language Analysis ✅

**Goal**: Polyglot detection + HTTP route extraction + cross-language graph linking.

**Status**: Complete (2026-02-28).

- [x] Polyglot repo detection: manifest scan, directory grouping, layer construction
- [x] Role classification: server/client/worker/library from source patterns + route fallback
- [x] HTTP route extraction via regex matchers for 7 languages (Go, TS, Python, Java, Rust, Ruby, C#)
- [x] Go 1.22+ mux pattern support ("GET /path" embedded method syntax)
- [x] Server-side patterns: HandleFunc, chi, gin, echo, Express, Flask, FastAPI, Spring, Rocket, Actix, Sinatra, Rails, ASP.NET
- [x] Client-side patterns: fetch, axios, requests, httpx, http.Get, http.NewRequest
- [x] Graph schema: Layer/Route vertices, HANDLES/FETCHES/BELONGS_TO edges
- [x] Cross-language linking: shared Route vertices connect backend handlers to frontend callers
- [x] 4 new Cypher templates: api_routes, cross_calls, layer_deps, polyglot_overview (14 total)
- [x] `dep_graph` cross_language parameter for Route edges in dependency output
- [x] `repo_analyze` deep mode adds "Cross-Language Architecture" section for polyglot repos
- [x] Route path normalization: strip scheme/host, replace params with *, case-insensitive

**Ref**: [MLSA (arxiv 1808.01213)](https://arxiv.org/abs/1808.01213) — monolingual graphs stitched at FFI boundaries; [rustic-ai/codeprism](https://github.com/rustic-ai/codeprism) — `EdgeKind::RoutesTo` for HTTP API boundaries.

**Deliverable**: Cross-language API boundary linking via Layer/Route graph. ✅

---

## v1.10: Analysis Quality + Graph Enrichment + AST Diff ✅

**Goal**: Smarter ranking, better LLM prompts, structured output, richer graph schema, AST-level diffs.

**Status**: Complete (2026-02-28). Bundled release — 6+7+8+9 work delivered together.

### Analysis quality (6.1–6.9) ✅
- [x] XML prompt format: `<query>`, `<file-tree>`, `<symbols>`, `<file path="...">`
- [x] `⋮...` skeleton markers with `│` line prefix
- [x] BM25F scoring with field weights: symbol names ×5, file path ×3, content ×1
- [x] Query understanding: camelCase/snake_case splitting, acronym handling
- [x] PageRank on import graph (damping=0.85, 20 iterations), combined 70% BM25F + 30% PageRank
- [x] Intent-aware system prompts (5 intents: architecture, debug, navigate, dependency, general)
- [x] `format=json` structured response envelope for `repo_analyze`
- [x] Contextual file annotations (`<!-- imported by N files, M symbols, lang -->`)

**Where**: `internal/ranking/`, `internal/analyze/context.go`, `internal/render/render.go`, `cmd/go-code/tool_repo_analyze.go`.

### Graph enrichment (7.1–7.4) ✅
- [x] Schema injection in freeform Cypher prompt
- [x] IMPORTS edges: File → Package from tree-sitter parsed data
- [x] Composite quality grade (A-F) in RepoMetrics
- [x] Hotspot scoring: `percentile(churn) × percentile(complexity)`
- [x] Import diff with categorization and framework detection

**Where**: `internal/codegraph/`, `internal/compare/`.

### AST structural diff (8.1–8.2) ✅
- [x] `smacker/gum` integration with tree-sitter adapter
- [x] Function-level AST diff: Insert, Delete, Update, Move operations
- [x] Edit script wired into `code_compare` output
- [x] DiffStats aggregation across modified matches

**Where**: `internal/compare/astconv.go`, `internal/compare/astdiff.go`.

### Impact analysis ✅
- [x] `impact_analysis` MCP tool — blast radius computation
- [x] Depth-scored: direct callers (high risk), transitive (medium), downstream (low)

**Where**: `internal/impact/`, `cmd/go-code/tool_impact.go`.

**Deliverable**: Smart ranking (BM25F+PageRank), XML prompts, AST diff, graph enrichment, impact analysis. ✅

---

## v1.11: Type Hierarchy + Dead Code + Incremental Indexing ✅

**Goal**: Richer type system in graph, dead code detection, faster re-indexing.

**Status**: Complete.

- [x] INHERITS/IMPLEMENTS edges from parser type relationships (Go, Python, TS, Java)
- [x] Type hierarchy and subtypes Cypher templates
- [x] PageRank scoring integrated into Symbol vertices
- [x] Incremental indexing with file mtime tracking
- [x] `dead_code` MCP tool — detect functions with zero incoming calls
- [x] Type relationship extraction with Go, Python, TypeScript, Java support
- [x] Regex-based fallback tokenizer for unsupported languages
- [x] Complexity and lines metrics on Symbol vertices

**Deliverable**: Type hierarchy, dead code detection, incremental indexing. ✅

---

## v1.12: Code Search + Graph Improvements ✅

**Status**: Complete.

- [x] `code_search` MCP tool — grep-like search with regex, context lines, file globs
- [x] Framework-aware heuristics in `dead_code` to reduce false positives
- [x] Graph schema injection into classifier prompt
- [x] Example queries and AGE constraints in freeform Cypher prompt
- [x] Multiline Cypher handling fix in `countReturnCols`

---

## v1.13: Explore Tool + Graph Fixes ✅

**Status**: Complete.

- [x] `explore` MCP tool — quick repository overview (file tree + key symbols)
- [x] Cyclomatic complexity added to parser symbol output
- [x] Fix AGE-incompatible Cypher in dead_code and call_chain templates
- [x] Improved codegraph classifier accuracy and template quality
- [x] `case_sensitive` parameter for `code_search`

---

## v1.14: LLM-Free Architecture + Tool Polish ✅

**Goal**: Remove LLM dependency from core tools, switch to structured XML output, modernize infrastructure.

**Status**: Complete (2026-03-02). 33 commits since v1.13.0.

### repo_analyze V2 ✅
- [x] Removed LLM dependency — pure mechanical AST data in V2 XML
- [x] XML output format with structured `<response>/<repo>/<packages>/<symbols>` envelope
- [x] Filter generated code files (`.pb.go`, `.gen.go`, `_generated.go`)
- [x] Truncate long symbol signatures (>200 chars)
- [x] Large output saved to file, return summary with path
- [x] Dep summary section: external deps, fan-in/fan-out, cycles

### XML output for all tools ✅
- [x] All tools switched from JSON/text to structured XML
- [x] CDATA sections for source code and tree output
- [x] `dead_code`, `dep_graph`, `call_trace` converted to XML

### Code Health ✅
- [x] `code_health` MCP tool — grade (A-F), score (0-100), metrics, hotspots, type relationships
- [x] Reuses `compare.BuildSnapshot` + `ComputeMetrics` + `ComputeHotspots` + `ComputeRelStats`
- [x] Exported `GradeScore` for numeric score alongside letter grade
- [x] Fix: hotspot path mismatch (absolute vs relative)

### Explore enhancements ✅
- [x] README excerpt (first meaningful sentences)
- [x] Dep highlights (lightweight dep overview without LLM)
- [x] Health score (lightweight quality score from parsed symbols)
- [x] Content-based focus fallback: when focus matches no file paths, re-ingest and filter by symbol names, imports, and call sites (OR logic)
- [x] `FocusMode` field in Result (`"content"` when fallback used, empty otherwise)

### Reusable content-focus module ✅
- [x] Extracted `ingest/focus.go`: `ContentFilter`, `FilterFiles`, `ParseLightweight`, `ContentFallback`
- [x] Migrated `explore` and `code_compare` to shared module (eliminated 3 duplicate implementations)
- [x] Content fallback added to `BuildSnapshot` — fixes `code_compare` with semantic focus terms
- [x] 9 unit tests covering symbol/import/call matching, OR logic, case-insensitivity

### Tool improvements ✅
- [x] `call_trace`: compact mode (skip LLM narrative, tree-only output)
- [x] `code_search`: `exclude_glob` parameter, `query` alias for `pattern`
- [x] Focus keyword fallback — spaces = keywords matched against file path (case-insensitive)
- [x] Updated all tool descriptions to document keyword focus mode

### Infrastructure ✅
- [x] Migrated to `go-mcpserver.Run()` — eliminated MCP boilerplate
- [x] go-mcpserver v0.2.0 → v0.5.0 (SessionTimeout, MCPLogger)
- [x] go-kit integration: GenericCache → go-kit/cache, per-key TTL, circuit breaker
- [x] Resolved 28 golangci-lint issues across codebase
- [x] Anchored gitignore patterns (leading `/`)

**Deliverable**: LLM-free core analysis, XML output, content-based explore focus, 13 MCP tools fully polished. ✅

---

## v1.15: Identifier-Level Ranking ✅

**Goal**: Precision file ranking via identifier-level reference graph + personalized PageRank + multi-signal fusion.

**Status**: Complete (2026-03-03). Replaced package-level import-only PageRank with identifier-level call graph.

### Reference graph builder ✅
- [x] `RefGraph` type with weighted file→file edges from `ExtractCalls()` data
- [x] Ambiguous symbol resolution: weight = `1.0 / len(definers)` per call site
- [x] Merged edges: call edges + import edges combined, self-edges excluded
- [x] 6 unit tests

**Where**: `internal/ranking/refgraph.go`.

### Personalized PageRank ✅
- [x] `PersonalizedPageRank()` with seed-biased teleportation vector
- [x] Seeds from query-matched symbols: ×10 exact, ×1 substring
- [x] Falls back to uniform (standard PageRank) when no seeds
- [x] 5 unit tests

**Where**: `internal/ranking/personalized.go`.

### Fusion ranking ✅
- [x] `FusionRank()` combines N signals via min-max normalization to [0,1]
- [x] Three signals: BM25F (0.5) + PersonalizedPageRank (0.3) + ExactMatch (0.2)
- [x] Replaces hardcoded `bm25*0.7 + pageRank*100*0.3` without normalization
- [x] 5 unit tests

**Where**: `internal/ranking/fusion.go`.

### Pipeline integration ✅
- [x] Call sites extracted during file parsing (`fileParseResult.calls`)
- [x] Refactored `context.go` → `context.go` + `rank.go`
- [x] Integration test: "Process" query ranks definer > caller > unrelated

**Where**: `internal/analyze/rank.go`, `internal/analyze/analyze.go`.

**Ref**: [Aider-AI/aider](https://github.com/Aider-AI/aider) — personalized PageRank (α=0.85), ×10 query boost; [SimplyLiz/CodeMCP](https://github.com/SimplyLiz/CodeMCP) — FusionRanker with min-max normalization.

**Deliverable**: 3-signal fusion ranking with identifier-level PageRank. ✅

---

## v1.16: Multi-Language Analysis Hardening ✅

**Goal**: Bring Python and C++ analysis to the same quality as Go and Rust.

**Status**: Complete (2026-03-06).

### Python improvements ✅
- [x] `python.scm`: 5→8 patterns — decorated functions/classes/methods in all combinations
- [x] Module-level variable extraction, ALL_CAPS → KindConst promotion
- [x] Decorator extraction → Symbol.Attributes field
- [x] Visibility detection: `_name` = private, rest = public
- [x] `python_calls.scm`: decorator references as call sites + super()
- [x] Dead code: 55 dunder methods excluded + 12 framework decorator patterns (@property, @app.route, @pytest.fixture, etc.)
- [x] Results on MemDB (518 .py files): dead_code 14.4%→3.12%, 0 false positives

### C++ improvements ✅
- [x] `cpp.scm`: 37→123 lines — namespace, template class/function, typedef, using alias, global vars, struct methods
- [x] `cpp_rels.scm`: new — class/struct inheritance (simple + qualified base)
- [x] `cpp_calls.scm`: qualified calls, template calls, new expressions
- [x] Visibility detection via access_specifier (public/private/protected)
- [x] Attribute extraction: virtual, override, static, constexpr, inline, explicit, noexcept, friend
- [x] Dead code: exclude destructors, operator overloads, virtual/override, friend
- [x] Import categorization: STL→stdlib, boost/Qt/grpc→thirdparty, local→internal
- [x] Framework detection: boost, qt, grpc, gtest, opencv, eigen, spdlog, fmt

### Depth alias normalization ✅
- [x] `NormalizeDepth()`: maps LLM hallucinations (full→deep, shallow→overview) to canonical values

**Deliverable**: Python and C++ now on par with Go/Rust analysis quality. ✅

---

## v1.17: Semantic Code Search ✅

**Goal**: Find code by meaning, not just name patterns.

**Status**: Complete (2026-03-06). Jina Code V2 embeddings + pgvector search + hybrid RRF + graph expansion + auto-indexing.

### Embedding model ✅
- [x] **Jina Code V2** (jina-embeddings-v2-base-code): 768-dim, 161M params, optimized for 30 programming languages
- [x] Served via memdb-go `/v1/embeddings` with multi-model registry (e5-large stays for MemDB memory)
- [x] ONNX INT8 quantized (154MB), no prefix needed (unlike e5's "passage: ")
- [x] 3.5x faster than multilingual-e5-large (89s vs ~3min for 95 functions)

### Embedding client ✅
- [x] Client for memdb-go `/v1/embeddings` (jina-code-v2, 768-dim)
- [x] Batch embedding: configurable batch size (default 32), parallel requests
- [x] No prefix manipulation — Jina handles code natively

**Where**: `internal/embeddings/client.go`.

### Embedding storage ✅
- [x] Standalone `code_embeddings` table with pgvector `vector(768)` column
- [x] Schema: repo, file_path, symbol_name, language, signature, body, embedding
- [x] Cosine distance search via `<=>` operator

**Where**: `internal/embeddings/store.go`.

### Embedding pipeline ✅
- [x] Embed function/method bodies during `explore` tool indexing
- [x] Content hash tracking — skip unchanged symbols on re-index
- [x] Batch upsert with ON CONFLICT for incremental updates

**Where**: `internal/embeddings/indexer.go`.

### `semantic_search` tool ✅
- [x] NL query → embed → cosine similarity → top-K results
- [x] Input: query text + repo + optional language filter + top_k
- [x] Output: ranked list of functions with similarity score + file path + symbol name

**Where**: `cmd/go-code/tool_semantic_search.go`, `internal/embeddings/`.

### Hybrid search (RRF) ✅
- [x] Reciprocal Rank Fusion merging semantic + keyword results (k=60)
- [x] Keyword search via `codesearch.Search` (literal, case-insensitive)
- [x] `MatchKeywordHits`: maps file:line keyword hits to nearest indexed symbol in pgvector
- [x] Items found by both methods get boosted RRF scores
- [x] 5 unit tests covering merge, empty inputs, topK clamping

**Where**: `internal/embeddings/rrf.go`, `internal/embeddings/store_keyword.go`, `cmd/go-code/tool_semantic_search_hybrid.go`.

### Graph expansion ✅
- [x] `Expander`: queries Apache AGE for 1-hop CALLS neighbors (forward + reverse)
- [x] Dedup against existing results, max 5 extra graph-sourced results
- [x] Graph-expanded symbols participate in RRF merge naturally
- [x] Graceful degradation: returns nil if graph missing or AGE unavailable
- [x] Inline name filter (AGE does not support parameterized arrays)

**Where**: `internal/embeddings/expand.go`.

### Auto-indexing ✅
- [x] `AutoIndex`: scans `AUTO_INDEX_DIRS` for git repos at startup
- [x] Sequential indexing (one repo at a time) to avoid overwhelming embedding API
- [x] Runs in background goroutine, does not block server startup
- [x] Skips already-indexed repos via content hash (instant skip)

**Where**: `internal/embeddings/autoindex.go`, `cmd/go-code/register.go`.

### Hard red TTL tests ✅
- [x] 8 codegraph meta tests: sub-second boundaries, just-expired, far future, max/overflow TTL, config defaults
- [x] 6 cache tests: TTL boundary, update resets expiry, expired eviction, zero/negative TTL, stalest-first eviction

**Where**: `internal/codegraph/meta_test.go`, `internal/cache/cache_test.go`.

- [ ] Benchmark: semantic vs keyword search on known queries

**Ref**: [code-graph-rag](https://github.com/vitali87/code-graph-rag) — UniXcoder embeddings + vector DB; [CodeCompass (arxiv 2602.20048)](https://arxiv.org/abs/2602.20048) — graph-based navigation achieves 99.4% task completion vs 76.2% baseline.

**Deliverable**: NL-powered code search with hybrid RRF merge, graph expansion, and auto-indexing. ✅

---

## v1.18: Type-Aware Analysis ✅

**Goal**: Precision enhancement for Go repos via compiler-level intelligence.

**Status**: Complete (2026-03-16). go/types integration + compound tools + 3-tier degradation.

**Approach changed from SCIP to go/types**: Research showed go/types (direct `go/packages.Load`) is simpler, has no external dependencies, and provides equivalent precision for Go repos. SCIP can be added later as an optional backend.

### Go type-aware resolution ✅
- [x] `internal/goanalysis/loader.go` — `go/packages.Load` wrapper with timeout, go.mod validation
- [x] `internal/goanalysis/resolver.go` — `go/types`-based call resolver with interface dispatch via `types.Implements`
- [x] `internal/callgraph/convert.go` — TypedEdge→CallEdge bridge + MergeCallGraphs
- [x] `internal/callgraph/repo.go` — `BuildFromRepo` enhanced: auto-detects Go modules, attempts go/types resolution, falls back to tree-sitter silently

### 3-tier degradation system ✅
- [x] `internal/tier/tier.go` — Basic (tree-sitter) / Enhanced (go/types) / Full (go/types+VTA)
- [x] `DegradationWarning` with `CapabilityPct` and fix instructions
- [x] `Provenance` metadata tracking which backends contributed
- [x] Tier propagated to `call_trace`, `impact_analysis`, `dead_code` XML/JSON output

### Compound tools ✅
- [x] `understand` MCP tool — symbol deep-dive aggregating callees + callers + complexity
- [x] `prepare_change` MCP tool — pre-change risk assessment aggregating impact + dead code
- [x] Ambiguity handling: returns disambiguation hints when multiple symbols match
- [x] Semantic suggest fallback when symbol not found

**Ref**: [CodeMCP/CKB](https://github.com/SimplyLiz/CodeMCP) — 3-tier system, compound tools; [golang.org/x/tools/go/callgraph](https://pkg.go.dev/golang.org/x/tools/go/callgraph) — VTA algorithm.

**Deliverable**: Type-aware Go analysis, 2 compound tools, 3-tier degradation. 18 MCP tools total. ✅

---

## v1.19: Diff-Aware Review

**Goal**: Git-integrated change analysis — detect changed symbols, compute differential impact, generate review context with risk guidance. Inspired by [code-review-graph](https://github.com/tirth8205/code-review-graph) blast-radius approach but built on go-code's superior backend (AGE graph, type-aware analysis, ox-codes search).

**Status**: Planned.

### 19.1 Git layer (`internal/git/`)
- [ ] `ChangedFiles(repoRoot, base string) ([]string, error)` — `git diff --name-only` between base and HEAD
- [ ] `ParseUnifiedDiff(repoRoot, base string) ([]FileDiff, error)` — changed lines per file (added/removed/modified ranges)
- [ ] `ChangedSymbols(diffs []FileDiff, symbols []Symbol) []ChangedSymbol` — intersect diff line ranges with parsed symbol spans
- [ ] Support base refs: commit SHA, branch name, `HEAD~N`, tag
- [ ] Fallback: `git diff --cached` for staged changes when no base specified
- [ ] Unit tests: mock git output, verify symbol-diff intersection

**Where**: `internal/git/diff.go`, `internal/git/symbols.go`.

### 19.2 TESTED_BY edges in graph
- [ ] Extract test→symbol mappings during graph build (`internal/codegraph/graph_build.go`)
- [ ] Go: `TestXxx` / `Test_Xxx` → function `Xxx` (same package)
- [ ] Python: `test_xxx` → function `xxx`; class `TestXxx` → class `Xxx`
- [ ] TS/JS: `describe("Xxx")` / `it("should xxx")` → nearest matching symbol
- [ ] Generic fallback: test file `*_test.*` → symbols in corresponding source file
- [ ] `TESTED_BY` edge label in AGE schema + Cypher template `tests_for`
- [ ] Unit tests: verify edge creation for Go, Python, TS test patterns

**Where**: `internal/codegraph/tested_by.go`, `internal/codegraph/schema.go`.

### 19.3 `review_delta` MCP tool
- [ ] Input: `repo` (GitHub/local), `base` (default `HEAD~1`), `depth` (default 2), `include_snippets` (bool)
- [ ] Pipeline: git diff → changed symbols → impact_analysis per symbol → aggregate → risk guidance
- [ ] Output XML:
  - `<changed_files>` — list with added/removed/modified line counts
  - `<changed_symbols>` — name, kind, file, change type (added/modified/removed)
  - `<impacted_symbols>` — transitive callers/callees within depth
  - `<untested>` — changed symbols lacking TESTED_BY edges
  - `<risk_guidance>` — flags: wide blast radius (>20 nodes), inheritance changes, cross-package impact, untested changes
  - `<source_snippets>` — optional context around changed symbols (3 lines before, 1 after)
- [ ] Token-aware truncation: configurable max output size, prioritize high-risk items
- [ ] Integration with existing `impact.Analyze()` and `codegraph` TESTED_BY queries
- [ ] Unit tests + integration test on go-code's own repo

**Where**: `internal/review/delta.go`, `cmd/go-code/tool_review_delta.go`.

### 19.4 `review_pr` MCP tool (stretch)
- [ ] Input: `repo`, `pr_number` — fetch PR diff via forge API (GitHub/GitLab)
- [ ] Reuse `review_delta` pipeline with PR diff as input
- [ ] Add PR metadata: title, description, author, labels, changed file count
- [ ] Risk summary with actionable review checklist

**Where**: `cmd/go-code/tool_review_pr.go`.

**Ref**: [code-review-graph](https://github.com/tirth8205/code-review-graph) — SQLite graph + BFS blast radius + review context (avg 6.8x token reduction); go-code advantage: AGE graph (Cypher queries, PageRank), type-aware Go analysis, hidden caller detection via ox-codes, 3-tier degradation.

**Deliverable**: `review_delta` + `review_pr` tools — diff-aware impact analysis with risk guidance. 20 MCP tools total.

---

## Dependencies

```
v1.0 (Foundation) ✅ ──→ v1.1–v1.4 (Structure) ✅ ──→ v1.5 (Comparison) ✅
                                                              │
                              ┌────────────────────────────────┤
                              ▼                                ▼
                    v1.6 (Call Trace) ✅              v1.7 (Migration) ✅
                    v1.8 (Code Graph) ✅
                    v1.9 (Cross-Lang) ✅
                              │
                              ▼
                    v1.10 (Quality + Graph + AST Diff) ✅
                    v1.11 (Type Hierarchy + Dead Code) ✅
                    v1.12 (Code Search) ✅
                    v1.13 (Explore) ✅
                    v1.14 (LLM-Free + Polish) ✅
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      v1.15 Identifier  v1.16 Multi-Lang  v1.17 Semantic Search ✅
         Ranking ✅       Hardening ✅     (Jina V2 + Hybrid RRF
       (7 tasks)        (Python+C++)      + Graph Expand + Auto-Index)
                                               │
                                          v1.18 Type-Aware ✅
                                           Analysis (go/types
                                           + compound tools)
                                               │
                                          v1.19 Diff-Aware
                                           Review (git diff
                                           + TESTED_BY + delta)
```

**Completed**: v1.0 through v1.18 (18 tools, 9 languages, type-aware Go analysis, compound tools, 3-tier degradation).
**Planned**: v1.19 (diff-aware review — `review_delta` + `review_pr` tools).



## v1.20: AGE Graph Build Performance — Direct COPY INSERT

**Goal:** Reduce code_graph first-build time from ~90s to <15s for large repos by replacing Cypher-layer writes with direct SQL `COPY FROM STDIN` into AGE's internal PostgreSQL tables.

**Background:** v1.8 introduced code_graph (AGE). v1.19.5 investigated performance and found the Cypher layer (UNWIND+MERGE) is the bottleneck — AGE parses and executes Cypher queries for each batch. Direct SQL INSERT bypasses this entirely.

**Research findings (2026-04-24):**
- AGE vertex tables: `{graphName}.{Label}(id graphid, properties agtype)`
- AGE edge tables: `{graphName}.{Edge}(id graphid, start_id graphid, end_id graphid, properties agtype)`
- `graphid` accepts integer string input: `'1970324836974593'::graphid` ✓
- `agtype` accepts JSON string input: `'{"name": "func"}'::agtype` ✓
- `graphid = (label_id << 48) | seq_num` — fully deterministic, no DB round-trips
- Text-format COPY FROM STDIN works for both types → no shared volume needed
- After COPY, must `setval(label_seq, count)` to advance AGE sequences

**Expected results:**
- Vertices (8700 for memdb): UNWIND ~38s → COPY ~2s (**19x**)
- Edges (33000 for memdb): UNWIND ~41s → COPY ~2s (**20x**)
- Total build: ~90s → ~12s (**7.5x**)

### 20.1 Proof of concept ✅ (pre-implementation validated)
- [x] `'integer'::graphid` confirmed working via psql
- [x] `'{"json":"string"}'::agtype` confirmed working
- [x] `COPY table FROM STDIN (FORMAT text)` approach validated
- [x] graphid bit-layout confirmed: label_id=7 (Symbol), seq=1 → id=1970324836974593

### 20.2 `BulkCopyInsert` implementation
- [ ] `internal/codegraph/copy_insert.go` — core COPY logic
- [ ] `queryLabelMetas` — fetch label_id + seq_name from ag_catalog.ag_label
- [ ] `copyVertices` — stream vertex rows via PgConn.CopyFrom text format
- [ ] `copyEdges` — stream edge rows with pre-computed graphid endpoint lookup
- [ ] `advanceSequences` — setval each label sequence after COPY
- [ ] Fallback to UNWIND on any error (non-fatal)

### 20.3 Wire in IndexRepo
- [ ] Replace insertBatches/insertEdgeBatches with BulkCopyInsert
- [ ] Keep UNWIND fallback for resilience
- [ ] Benchmark: piter-server (9 files) + memdb (950 files)

**Deliverable**: code_graph first-build <15s for memdb. Zero Cypher writes. Full Cypher readability preserved.

**Plan**: `~/deploy/my-server/plans/go-code/2026-04-24-age-direct-copy-insert.md`

---

## v1.19.x: AGE Stability Fixes (Shipped 2026-04-24)

These fixes were discovered and shipped while debugging code_graph performance:

- **v1.19.1** `ed0325b` — AGE stability: CREATE EXTENSION age + 42P01 cache miss + single-vertex inserts + background goroutine for first-build + sync.Map race prevention + Dockerfile scip-go reuse
- **v1.19.2** `33c52f8` — CypherWriter interface + BulkWriter (synchronous_commit=off) + timing instrumentation in IndexRepo
- **v1.19.3** `4bf0eed` — UNWIND batch inserts for vertices (group by label) and edges (UNWIND+MATCH+MERGE, confirmed working)  
- **v1.19.4** `97a197c` — Pre-edge EnsureIndexes + disable statement_timeout in BulkWriter
- **v1.19.5** `8c7148a` — Pre-INSERT EnsureIndexes (GIN indexes before vertex inserts eliminates O(N²) MERGE scan)
- **v1.19.6** `60cca65` — Adaptive UNWIND batch size per label (caps Cypher query at 8KB)
- **v1.19.7** `bfb64d0` — storeFileMtimes via pgx.CopyFrom (948 sequential INSERTs → 1 COPY, 280x faster)

**Cumulative improvement**: code_graph first-build 6+ min → ~90s. Queries from cache: 2-3s.

## v1.21: OTel Function Attribution + debug_investigate Polish (Shipped 2026-05-09)

**Goal**: Industry-first Go OTel HTTP middleware with function-level handler attribution. Closes the metrics→trace→file:line loop in `debug_investigate` Tier-1 symbol resolution.

**Status**: Shipped 2026-05-09. Empirically validated on go-code + dozor + oxpulse-chat (Rust) + partner-edge-sfu (Rust).

### 21.1 OTel handler attribution (`anatolykoptev/go-kit#49,#50,#51,#52,#53,#6`)
- [x] `RegisterRoute(method, pattern, fn)` registry — startup-time `runtime.FuncForPC` capture
- [x] Drop-in `httpmw.NewServeMux()` wrapper — auto-registers via `Handle`/`HandleFunc` interception
- [x] `WalkAndRegister(walkFn)` — chi.Walk and gorilla.Walk integration
- [x] `RegisterGinRoute(method, pattern, name)` — gin uses HandlerFunc string directly
- [x] `-fm` suffix strip for method-value wrappers
- [x] Closure → `code.filepath`+`code.lineno` graceful fallback
- [x] `tracing/slogh` package — slog handler injecting `trace_id`+`span_id` into records (port from tilegroxy)
- [x] `theme.css` canonical token export, `FontPreload.astro` helper, `maskIconColor` prop, `--aw-color-primary` legacy leak removed (kit-side fixes from review)

### 21.2 debug_investigate Tier-1 wiring (`anatolykoptev/go-code#87,#88,#89,#90,#91,#92,#93,#94,#95,#96,#97,#98,#99`)
- [x] B4 downstream callees walk (top-1 hypothesis, depth 2, 0.3/depth scoring)
- [x] B5 body extraction topN 3 → 5
- [x] OTel self-instrumentation (Setup + httpmw + mcpmw)
- [x] go-kit v0.50 → v0.53 bump
- [x] Drop-in NewServeMux refactor — single `(*githubWebhookHandler).ServeHTTP` symbol
- [x] slogh deadlock fix (concrete handler base, not slog.Default)
- [x] InfoContext threading for span context propagation
- [x] `code.function` joined into Tier-1 hypothesis subject (was namespace-only)
- [x] Path mapping: `/build/...` → host repo root via `repo` arg
- [x] Polling instruction 30s → 5s (perceived latency 6× faster)
- [x] LLM HTTP timeout 10s → 30s (catches cliproxyapi long-tail)
- [x] Service→repo dir mapping (e.g. `partner-edge-sfu` → `oxpulse-partner-edge`)

**Empirical validation**:
- go-code `POST /webhook/github` → `code.function=(*githubWebhookHandler).ServeHTTP, code.filepath=/build/cmd/go-code/webhook_github.go:25`
- dozor `GET /health` → `code.function=runGateway.healthHandler.func5, code.lineno=162`
- oxpulse-chat (Rust): 4 routes resolved (`ws_call`, `http_request`, `metrics_handler`, `spa_fallback`)
- partner-edge-sfu (Rust): `oxpulse_sfu::client_ws::handler:185` + body excerpts

**State of the art comparison** (research-confirmed):
DataDog APM, Beyla, OTel auto-instrumentation, Honeycomb, Pixie, Coroot, OTel-go-contrib all stop at HTTP route templates. `tilegroxy` is the only public Go project emitting `code.*` on HTTP server spans, and they hardcode strings — not runtime reflection. Krolik is the first Go middleware doing function-level attribution at runtime.

---

## Releases

| Tag | Commit | What |
|-----|--------|------|
| v1.0.0 | `c4a5c55` | Foundation — 5 MCP tools, Go/Python/TypeScript parsing |
| v1.1.0 | `5e75bc5` | 6 new languages (Rust, Java, C, C++, Ruby, C#) |
| v1.2.0 | `cb0fc1f` | Noise reduction, test filtering, symbol limits, dep_graph fixes |
| v1.3.0 | `24613ba` | Render modes (signatures, skeleton, focused) for `repo_analyze` |
| v1.3.1 | `72e8617` | Fix render bugs: dangling braces, nested symbols, validation |
| v1.4.0 | `a99d14d` | Multi-level analysis (depth) + LRU caching |
| v1.5.0 | `4e471f0` | Comparison Engine — `code_compare` with structural diff + LLM analysis |
| v1.5.1 | `eb70fe0` | Fix 6 bugs found during practical testing of `code_compare` |
| v1.6.0 | `36f2144` | Call chain tracing — `call_trace` with bidirectional BFS + LLM narrative |
| v1.7.0 | `07e8907` | go-search migration — `repo_search`, `repo_analyze` quick/issues modes |
| v1.8.0 | `127fd2d` | Code graph — `code_graph` with Apache AGE, NL→Cypher + LLM freeform |
| v1.9.0 | `b06e340` | Cross-language analysis — polyglot detection, HTTP route extraction |
| v1.10.0 | `4d48293` | Analysis quality + graph enrichment + AST diff + impact analysis |
| v1.11.0 | `c8d7a25` | Type hierarchy + dead code + incremental indexing |
| v1.12.0 | `13da1d0` | `code_search` tool + graph improvements |
| v1.13.0 | `8eaf53f` | `explore` tool + codegraph fixes |
| v1.14.0 | `0af48a3` | LLM-free architecture, XML output, `code_health`, explore content fallback |
| v1.15.0 | `780139d` | Identifier-level fusion ranking (PPR + BM25F + exact match) |
| v1.19.1–v1.19.7 | `ed0325b`–`472ce61` | AGE stability: CREATE EXTENSION, 42P01 fix, BulkWriter, UNWIND batching, EnsureIndexes before inserts |
| v1.20.0 | `79b8791` | BulkCopyInsert — direct COPY INTO AGE tables; IndexRepo 6 min → 14s |
| v1.21.0 | `60add60`–`15b045d` | Three-layer search (pg_trgm+CE), dead code CE scores, code_health cache, pool fix |

## Technical Debt Watch

- [ ] tree-sitter grammar version pinning (test after upgrades)
- [ ] CGO cross-compilation for ARM64 Docker builds
- [ ] Memory usage profiling for large repos (10K+ files)
- [ ] Rate limiting for GitHub API calls
- [ ] Cognitive complexity: nesting depth penalty (cyclomatic done in v1.13)
- [ ] AST diff visualization: structured JSON output with edit operations, summary statistics, similarity score
- [x] Content-focus deduplication: `ingest/focus.go` shared module (3 duplicate implementations eliminated)
- [x] Cache eviction strategy for long-running server (LRU + TTL via go-kit/cache)
- [x] MCP boilerplate elimination (migrated to go-mcpserver.Run())
- [x] MCP SDK v1.4.0 output schema compatibility
- [x] jsonschema tag format (fixed: jsonschema_description)
- [x] Consistent versioning scheme (re-tagged: v1.0.0 → v1.4.0)
- [x] golangci-lint clean (28 issues resolved)
- [x] code_health timeout on large repos (background cache + freshness tuning)
- [x] semantic_search recall for code abbreviations (pg_trgm + CE reranker)
- [x] dead code false positives (CE cross-encoder confidence scores)
- [x] pgxpool exhaustion under concurrent builds (MaxConns=10)

