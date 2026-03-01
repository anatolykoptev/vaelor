# go-code Implementation Roadmap

## Phase 1: Foundation — Parse & Analyze (MVP) ✅

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
- [x] Signatures-only mode (Phase 2.2)
- [x] Skeleton mode with `...` placeholders (Phase 2.2)

### 1.4 LLM analysis ✅
- [x] LLM client via CLIProxyAPI (OpenAI-compatible)
- [x] System prompts for repo analysis, code comparison, dep graph
- [x] LLM context builder with 150K char budget
- [x] File prioritization by query relevance + import frequency + symbol count
- [x] Multi-level analysis: overview → module → deep (Phase 2.3)
- [ ] JSON output parsing with fallback (deferred)

### 1.5 MCP tools ✅
- [x] `repo_analyze` — ingest → parse → clean → LLM → structured answer
- [x] `file_parse` — tree-sitter AST/symbol extraction for single files
- [x] `symbol_search` — wildcard pattern matching across repos
- [x] `dep_graph` — import graph in mermaid/dot/json formats
- [x] `code_compare` — registered as stub (Phase 3)
- [x] Support GitHub repos (clone) and local paths
- [x] Health endpoint (`/health`)
- [x] Docker build and deploy (docker-compose + MCP registration)

**Deliverable**: 5 MCP tools on :8897. `repo_analyze` + `file_parse` + `symbol_search` + `dep_graph` working. ✅

---

## Phase 2: Structure — Enhanced Parsing & Cleaning

**Goal**: Improve code understanding quality. Additional languages, smarter cleaning modes, caching.

### 2.1 Additional languages ✅

**Status**: Complete (2026-02-28). 6 new languages added, 12 parser tests passing.

- [x] Rust handler (`handler_rust.go` + `rust.scm`) — functions, methods, structs, enums, traits, type aliases, consts, statics
- [x] Java handler (`handler_java.go` + `java.scm`) — classes, interfaces, enums, methods, constructors
- [x] C handler (`handler_c.go` + `c.scm`) — functions, structs, enums, typedefs, macros, globals
- [x] C++ handler (`handler_cpp.go` + `cpp.scm`) — extends C with classes, methods, namespaces, templates, qualified identifiers
- [x] Ruby handler (`handler_ruby.go` + `ruby.scm`) — methods, singleton methods, classes, modules, constants
- [x] C# handler (`handler_csharp.go` + `csharp.scm`) — classes, interfaces, structs, enums, methods, constructors, namespaces

**Total supported languages**: Go, Python, TypeScript/JS, Rust, Java, C, C++, Ruby, C# (9 languages).

### 2.2 Advanced cleaning modes ✅

**Status**: Complete (2026-02-28). New `internal/render` package, `mode` parameter on `repo_analyze`.

- [x] Signatures-only mode: extract API surface without bodies
- [x] Skeleton mode: structure with `// ...` placeholders
- [x] Focused mode: full bodies for query-relevant symbols, signatures for rest
- [x] Structural kinds (struct/interface/class/type) always preserve full body
- [x] Exposed as `mode` parameter on `repo_analyze` MCP tool

### 2.3 Multi-level analysis ✅

**Status**: Complete (2026-02-28). `depth` parameter on `repo_analyze`, three analysis levels.

- [x] Level 1 (overview): file tree + symbol signatures only (50K budget, signatures render mode)
- [x] Level 2 (module): selected files + dependency graph in Mermaid (150K budget, skeleton render mode)
- [x] Level 3 (deep): full function bodies for relevant symbols (200K budget, focused render mode)
- [x] Depth-aware system prompts (`SystemPromptForDepth`)
- [x] Default render mode inferred from depth (explicit `mode` overrides)
- [x] Dependency graph section injected between symbol summary and file contents

### 2.3a Noise reduction & quality fixes ✅

**Status**: Complete (2026-02-28). Released as v0.2.0.

- [x] `testdata/` added to default ignore dirs (all tools benefit)
- [x] `ExcludeTests` option — `symbol_search` and `dep_graph` skip `_test.go` files; `repo_analyze` keeps them for full picture
- [x] `symbol_search` result limit (default 100, max 500) — prevents unbounded output
- [x] `dep_graph`: stdlib imports filtered by default (`IncludeStdlib` opt-in)
- [x] `dep_graph`: self-import edges removed (test files importing own package)
- [x] `dep_graph`: format validation — unknown format returns error instead of silent fallback
- [x] Go parser: skip function-local var/const declarations (only package-level symbols)
- [x] Go parser: const block signature shows individual spec, not `const (`

### 2.4 Caching & performance ✅

**Status**: Complete (2026-02-28). New `internal/cache` package with LRU caches.

- [x] LLM response cache by FNV-1a hash of (systemPrompt + userPrompt), TTL 1h, LRU eviction (500 entries)
- [x] Parsed AST cache by (filePath, modTime, size) key, LRU eviction (5000 entries)
- [x] Configurable via env: `PARSE_CACHE_SIZE`, `LLM_CACHE_SIZE`, `LLM_CACHE_TTL_MIN`
- [x] 14 unit tests: get/set, TTL expiry, LRU eviction, staleness, concurrent access
- [ ] Git change frequency for file relevance ranking (deferred — using symbol count proxy)

**Deliverable**: Better analysis quality, more languages, faster repeated queries. ✅ Phase 2 complete.

---

## Phase 3: Comparison Engine ✅

**Goal**: Compare implementations between repositories to find the better solution.

**Status**: Complete (2026-02-28). `code_compare` tool fully operational.

### 3.1 Structural diff ✅
- [x] Symbol-level matching: exact (name+kind), fuzzy (Levenshtein, threshold 0.7), semantic (LLM classifier)
- [x] Side-by-side alignment of matched symbols with match score
- [x] Coverage gap detection (symbols missing from one side)
- [x] Metrics: avg/max function lines, test ratio, doc ratio, error handling ratio, interfaces, external deps (9 signals)

### 3.2 `code_compare` tool ✅
- [x] Input: 2 repos (GitHub or local) + query + optional focus/language filter
- [x] Parallel snapshot building (goroutine worker pool)
- [x] Three-pass symbol matching (exact → fuzzy → semantic)
- [x] LLM analysis: quality verdicts, coverage gaps, architecture insights, recommendations
- [x] JSON output structured for AI consumption (CompareResult)
- [x] Budget-aware LLM context assembly (180K chars, 3K per snippet, 80 matched pairs, 40 gaps)

### 3.3 Module-level comparison ✅
- [x] Focus parameter for subdirectory-level comparison
- [x] Language filter for cross-language comparison
- [x] Quality-focused LLM prompt: finds better solution, not just differences

**Deliverable**: `code_compare` tool that compares two implementations and finds the better one. ✅

---

## Phase 4: Advanced Analysis

**Goal**: Deeper understanding of code.

### 4.1 Call chain tracing ✅

**Status**: Complete (2026-02-27). `call_trace` MCP tool operational.

- [x] Call extraction via tree-sitter queries for all 9 languages (separate `*_calls.scm` files)
- [x] Name-based resolution: same-file → same-package → cross-package
- [x] BFS trace with configurable depth (default 5, max 10)
- [x] Bidirectional: callees (forward) and callers (reverse)
- [x] Cycle detection (marks cycles, avoids infinite loops)
- [x] LLM narrative explanation of execution flow
- [x] `call_trace` MCP tool with JSON output

### 4.2 Code graph ✅

**Status**: Complete (2026-02-28). `code_graph` MCP tool operational with Apache AGE.

- [x] Store symbols + relationships in Apache AGE (separate `gocode` database)
- [x] Schema: Package/File/Symbol vertices + CONTAINS/CALLS edges
- [x] 10 Cypher query templates (who_calls, calls_of, imports_of, symbols_in, etc.)
- [x] NL → Cypher hybrid: template classification + freeform LLM generation fallback
- [x] Lazy indexing with TTL cache (1h local, 24h remote)
- [x] Read-only guard on freeform Cypher (blocks writes)
- [x] `code_graph` MCP tool with JSON + LLM narrative output

### 4.3 Cross-language analysis ✅

**Status**: Complete (2026-02-28). Polyglot detection + HTTP route extraction + cross-language graph linking.

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

**Deliverable**: Deep analysis capabilities — call chain tracing, code graph, cross-language API boundary linking. ✅

---

## Phase 5: go-search Migration ✅

**Goal**: Remove code tools from go-search, point to go-code.

**Status**: Complete (2026-02-28). All code tools migrated to go-code, removed from go-search.

### 5.1 New infrastructure ✅
- [x] `internal/retry` — generic exponential backoff with jitter
- [x] `internal/metrics` — atomic operation counters
- [x] `internal/cache` — GenericCache[T] with Redis L2 (go-redis/v9)
- [x] `internal/llm` — retry + fallback API keys + CompleteRaw
- [x] `internal/github` — SearchCode, SearchIssues, SearchRepos, ExtractOwnerRepo
- [x] `internal/search` — SearXNG client with FilterByScore, DedupByDomain

### 5.2 New tool modes ✅
- [x] `repo_analyze` mode=quick — GitHub Code Search + LLM summary
- [x] `repo_analyze` type=issue/pr — GitHub Issues/PR search + LLM analysis
- [x] `repo_search` — parallel SearXNG + GitHub API search, enrichment, LLM recommendations

### 5.3 go-search cleanup ✅
- [x] Remove `tool_github_repo_analyze.go` from go-search
- [x] Remove `tool_github_repo_search.go` from go-search
- [x] Remove `internal/gitingest/` from go-search
- [x] Remove code-specific functions from `sources/github.go`
- [x] Update go-search CLAUDE.md, tool count, metrics
- [x] Deploy both services, verify health

**Deliverable**: Clean separation. go-search = web search. go-code = code intelligence. ✅

---

## Phase 6: Analysis Quality ✅

**Goal**: Smarter ranking, better LLM prompts, structured output. Make existing tools produce better results.

**Status**: Complete (2026-02-28). All 9 subtasks implemented with tests.

### 6.1 XML prompt format ✅
- [x] Replace Markdown headings with XML tags: `<query>`, `<file-tree>`, `<symbols>`, `<file path="...">`
- [x] File blocks use `<file path=%q>` with properly quoted paths
- [x] Tests verify XML format and absence of old Markdown markers

**Where**: `internal/analyze/context.go` → `buildLLMContext()`, `formatFileBlock()`.

### 6.2 Skeleton markers ✅
- [x] `⋮...` marker replaces ambiguous `// ...` placeholder
- [x] `│` prefix on all visible lines in skeleton/signatures/focused modes
- [x] LLM gets explicit signal that code was intentionally truncated
- [x] 4 new tests, existing tests updated

**Where**: `internal/render/render.go` → `bodyPlaceholder`, `applyReplacements()`.

### 6.3 BM25F scoring ✅
- [x] BM25F with field weights: symbol names ×5, file path ×3, content ×1
- [x] IDF normalization: rare term matches score higher than common ones
- [x] Lazy document-frequency caching with substring matching
- [x] 6 unit tests in `internal/ranking/bm25_test.go`

**Where**: `internal/ranking/bm25.go`, `internal/analyze/context.go` → `prioritizeFiles()`.

### 6.4 Query understanding ✅
- [x] camelCase splitting: `handleUserAuth` → [handle, user, auth, handleuserauth]
- [x] snake_case splitting: `parse_file_content` → [parse, file, content, parse_file_content]
- [x] Acronym handling: `LLMClient` → [llm, client]
- [x] Digit boundaries: `Auth2Factor` → [auth, factor]
- [x] Keeps original compound term alongside subwords
- [x] 3 new tests

**Where**: `internal/analyze/context.go` → `extractQueryTerms()`, `splitCamelCase()`, `splitIdentifier()`.

### 6.5 PageRank on import graph ✅
- [x] Iterative PageRank (damping=0.85, 20 iterations) on file→file import graph
- [x] Dangling node handling for rank conservation
- [x] Combined scoring: 70% BM25F + 30% PageRank × 100
- [x] 6 unit tests (star, chain, cycle, normalization)

**Where**: `internal/ranking/pagerank.go`, `internal/analyze/context.go` → `prioritizeFiles()`.
**Note**: Current implementation uses file-level import edges. Phase 7.5 upgrades to identifier-level reference edges + personalized PageRank (Aider-style).

### 6.6 Intent-aware system prompts ✅
- [x] Classify queries into 5 intents: architecture, debug, navigate, dependency, general
- [x] Keyword-based scoring with deterministic tie-breaking
- [x] Specialized system prompts per intent (architecture, debug, navigate)
- [x] Depth overrides still apply (overview/deep modes)
- [x] 7 test functions

**Where**: `internal/llm/intent.go`, `internal/analyze/analyze.go` → `AnalyzeRepo()`.

### 6.7 + 6.8 Response envelope & structured output ✅
- [x] `format=json` parameter on `repo_analyze`: structured envelope with `schemaVersion`, `data`, `meta`, `suggestedNextCalls`
- [x] `format=text` (default) preserves existing human-readable output
- [x] Format validation rejects unknown values
- [x] Envelope types: `responseEnvelope`, `envelopeData`, `envelopeMeta`, `suggestedCall`

**Where**: `cmd/go-code/tool_repo_analyze.go`.

### 6.9 Contextual file annotations ✅
- [x] `<!-- imported by N files, M symbols, lang -->` annotations before each file block
- [x] Built from import counts and symbol counts — no LLM needed
- [x] Empty annotations omitted (no noise for files with nothing to say)
- [x] 2 new tests

**Where**: `internal/analyze/context.go` → `fileAnnotation()`, `appendFileContents()`.

**Deliverable**: Smarter file ranking (BM25F + PageRank), better LLM prompts (XML + intent), actionable output (envelope + structured JSON). ✅ All 6.1-6.9 complete.

---

## Phase 7: Graph Enrichment

**Goal**: Richer graph schema + faster re-indexing.

### 7.1 Schema injection in freeform Cypher
- [ ] Inject full graph schema (vertex labels, edge types, properties) into `SystemPromptGenerateCypher`
- [ ] Currently freeform Cypher generation has no schema context → hallucinated property names
- [ ] Test on 5+ NL queries that previously failed

**Ref**: [code-graph-rag](https://github.com/vitali87/code-graph-rag) — schema text injected into LLM prompt for Cypher grounding.

### 7.2 IMPORTS edges
- [ ] Extract import paths from existing tree-sitter parsed data (already in `@import.path` captures)
- [ ] Add `IMPORTS` edge type: File → Package (or File → File for relative imports)
- [ ] Add Cypher template: `imports_of` (what does file X import?), `imported_by` (who imports package Y?)
- [ ] Update graph schema documentation

**Ref**: [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext), [code-graph-rag](https://github.com/vitali87/code-graph-rag) — both store IMPORTS as first-class edges.

### 7.3 INHERITS / IMPLEMENTS edges
- [ ] New tree-sitter query captures: `@extends.base`, `@implements.interface` per language
- [ ] Go: interface satisfaction (type assertion patterns), struct embedding
- [ ] Python: `class Foo(Bar)`, Java/C#: `extends`/`implements`, TypeScript: `extends`/`implements`
- [ ] Rust: `impl Trait for Type`
- [ ] Add `INHERITS` and `IMPLEMENTS` edge types to `buildGraph()`
- [ ] Cypher templates: `hierarchy` (inheritance tree), `implementors` (who implements interface X?)

**Ref**: [rustic-ai/codeprism](https://github.com/rustic-ai/codeprism) — `EdgeKind::Extends`/`Implements`, strongly typed enums.

### 7.4 Incremental graph indexing
- [ ] Store FNV-64a hash per file in `code_graph_files` table (path, hash, indexed_at)
- [ ] On re-index: hash all files → diff against stored → parse only added/modified
- [ ] `DETACH DELETE` for removed files (cascade edges)
- [ ] Symbol-level diff for modified files (delete old symbols, insert new — not full file reindex)
- [ ] Persist hash state per graph, bump graph version to force rebuild on schema changes

**Ref**: [code-graph-rag](https://github.com/vitali87/code-graph-rag) — `FileHashCache` per file, JSON state; [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext) — file watcher + per-file reindex.

### 7.5 Identifier-level reference graph + fusion ranking
- [ ] Build identifier-level reference graph: extract all symbol definitions + references via tree-sitter (not just imports)
- [ ] File→file edges weighted by shared identifier references: `weight = mul / len(definers)` — distribute importance across multiple definition sites
- [ ] Weight multipliers: identifiers mentioned in query → ×10, long camelCase/snake_case (≥8 chars) → ×10, private (`_` prefix) → ×0
- [ ] Personalized PageRank: inject personalization vector boosting files that contain query-mentioned identifiers
- [ ] Seed expansion: if query mentions a struct/class name → auto-add its methods as seeds (from CodeMCP)
- [ ] Replace current import-only graph in `buildPageRankGraph()` with identifier-level reference graph
- [ ] Fusion ranking: combine BM25F + PageRank + exact symbol match + import depth into weighted score (currently only BM25F×0.7 + PageRank×0.3). Normalize each signal to [0,1] before combination.
- [ ] Fallback tokenizer: when tree-sitter finds definitions but no references for a language → regex-based `Token.Name` extraction (ensures graph edges exist for under-covered grammars)

**Ref**: [Aider-AI/aider](https://github.com/Aider-AI/aider) — `RepoMap` uses NetworkX MultiDiGraph with identifier-level edges + personalized PageRank (`alpha=0.85`); pygments fallback for reference extraction. [SimplyLiz/CodeMCP](https://github.com/SimplyLiz/CodeMCP) — `FusionRanker` combines 5 signals (FTS, PPR, Hotspot, Recency, Exact) with normalization; seed expansion via `expandSeedsWithMethods`. Our current Phase 6.5 PageRank uses file-level import edges only.

**Where**: `internal/ranking/pagerank.go` → personalization vector + seed expansion; `internal/ranking/fusion.go` → new multi-signal combiner; `internal/analyze/context.go` → `buildPageRankGraph()` → identifier-level graph; new `internal/parser/fallback.go` for regex tokenizer.

**Deliverable**: Richer graph queries (imports, inheritance), 10-100x faster re-indexing for local repos. Identifier-level ranking with personalized PageRank for more precise file prioritization.

---

## Phase 8: AST Structural Diff

**Goal**: True AST-level diff in `code_compare` using GumTree algorithm.

### 8.1 Integrate smacker/gum
- [ ] `go get github.com/smacker/gum` — same author as `smacker/go-tree-sitter`
- [ ] Use `gum/tsitter` adapter to convert tree-sitter CST → `gum.Tree`
- [ ] Verify tree-sitter version pin compatibility (same CGO dependency)

### 8.2 AST diff in code_compare
- [ ] `internal/compare/ast_diff.go` — function-level AST diff using `gum.Match()` + `gum.Patch()`
- [ ] Edit script output: Insert, Delete, Update, **Move** operations
- [ ] Move detection: function moved from file A to file B (not deleted+added)
- [ ] Integrate into existing `code_compare` output alongside symbol-level analysis
- [ ] LLM narrative enhanced with edit script data ("function X was moved", "function Y body changed")

### 8.3 Diff visualization
- [ ] Structured JSON output with edit operations
- [ ] Summary statistics: N moves, N updates, N deletes, N inserts
- [ ] Similarity score per matched function pair based on edit distance

**Ref**: [smacker/gum](https://github.com/smacker/gum) — Go GumTree implementation with tree-sitter adapter; [GumTreeDiff/gumtree](https://github.com/GumTreeDiff/gumtree) — academic reference (ICSE 2014); [Wilfred/difftastic](https://github.com/Wilfred/difftastic) — hash-before-compare optimization.

**Deliverable**: `code_compare` produces structural edit scripts with move detection, not just "similar symbols".

---

## Phase 9: Code Health & Impact Analysis

**Goal**: New tools for assessing code quality and change risk.

### 9.1 Complexity metrics
- [ ] Cyclomatic complexity via tree-sitter (count decision nodes: if/for/while/switch/case/&&/||)
- [ ] Cognitive complexity (nesting depth penalty)
- [ ] Per-function and per-file aggregation
- [ ] Expose as optional section in `repo_analyze` and `file_parse` output
- [ ] Hotspot detection: `percentile(churn) × percentile(complexity)` — product, not sum

**Ref**: [ast-metrics](https://github.com/halleck45/ast-metrics) — Go, tree-sitter metrics with HTML dashboards; [CodeMCP](https://github.com/SimplyLiz/CodeMCP) — `getFileComplexity`, `getHotspots`; [panbanda/omen](https://github.com/panbanda/omen) — hotspot = churn × complexity percentile product.

### 9.2 Impact analysis / blast radius
- [ ] New MCP tool: `impact_analysis` or extend `call_trace` with `mode=impact`
- [ ] Input: symbol name + repo → output: direct/indirect/transitive dependents
- [ ] Depth-scored: direct callers (high risk), transitive callers (medium), downstream (low)
- [ ] Confidence score degrades with distance from changed symbol
- [ ] Blast radius thresholds (from CodeMCP): low (≤2 modules / ≤5 callers), medium (≤5 / ≤20), high (above)
- [ ] Risk scoring: `visibility × impact_count` — public symbols score higher
- [ ] Uses existing CALLS edges from `code_graph` + call graph from `call_trace`
- [ ] LLM narrative: "changing this function affects N direct callers and M transitive dependents"

**Ref**: [Axon](https://github.com/harshkedia177/axon) — blast radius with confidence scores; [CodeMCP](https://github.com/SimplyLiz/CodeMCP) — `AnalyzeImpact` with SCIP FindReferences + BuildCallGraph, `ClassifyBlastRadius` with module/caller thresholds, telemetry-enhanced risk scoring.

### 9.3 Dead code detection
- [ ] Multi-pass approach: (1) build call graph, (2) identify entry points, (3) mark unreachable
- [ ] Entry point heuristics: main(), init(), exported functions, HTTP handlers, test functions
- [ ] Framework awareness: don't flag handler functions registered via reflect/decorators
- [ ] Output: list of candidate dead symbols with confidence level
- [ ] Expose as new MCP tool or mode on `symbol_search`

**Ref**: [Axon](https://github.com/harshkedia177/axon) — multi-pass with framework awareness; [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext) — dead code via graph analysis.

**Deliverable**: Complexity metrics, blast radius tool, dead code candidates.

---

## Phase 10: Semantic Code Search

**Goal**: Find code by meaning, not just name patterns.

### 10.1 Embedding infrastructure
- [ ] Embed function bodies during graph indexing via memdb-go `/v1/embeddings` (1024-dim)
- [ ] Store embeddings in pgvector column on Symbol vertices (or companion table)
- [ ] Batch embedding: group functions into batches of 32, parallel requests
- [ ] Cache embeddings per (file_hash, symbol_name) — skip unchanged functions

**Ref**: [code-graph-rag](https://github.com/vitali87/code-graph-rag) — UniXcoder embeddings + vector DB; [Octocode](https://github.com/Muvon/octocode) — GraphRAG + semantic search.

### 10.2 Semantic search tool
- [ ] New MCP tool: `semantic_search` — NL query → embed → cosine similarity → top-K results
- [ ] Input: query text + repo + optional language/file filter
- [ ] Output: ranked list of functions with similarity score + source snippet
- [ ] Hybrid mode: combine embedding similarity with name-pattern matching

### 10.3 Graph-enhanced search
- [ ] After finding semantically similar functions, expand via graph edges
- [ ] "Functions similar to X" + "functions that call similar functions" (graph walk)
- [ ] Re-rank by graph centrality (PageRank or degree)

**Ref**: [CodeCompass (arxiv 2602.20048)](https://arxiv.org/abs/2602.20048) — graph-based navigation achieves 99.4% task completion vs 76.2% baseline; agents need explicit prompting to use graph tools.

**Deliverable**: NL-powered code search that understands semantics beyond name matching.

---

## Phase 11: Type-Aware Analysis

**Goal**: Precision enhancement for Go repos via compiler-level intelligence.

### 11.1 SCIP backend for Go
- [ ] Optional `scip-go` integration for Go repos
- [ ] Parse SCIP index → extract precise CALLS/IMPLEMENTS/REFERENCES edges
- [ ] Merge SCIP data with tree-sitter data (SCIP primary, tree-sitter fallback)
- [ ] Stable symbol IDs from SCIP (survive renames)

**Ref**: [sourcegraph/scip](https://github.com/sourcegraph/scip) — Protobuf schema, Go bindings, streaming parser; [CodeMCP](https://github.com/SimplyLiz/CodeMCP) — SCIP as primary backend with tree-sitter fallback; [williamfzc/srctx](https://github.com/williamfzc/srctx) — Go tool combining SCIP + tree-sitter.

### 11.2 Go-native call graph enhancement
- [ ] Optional `golang.org/x/tools/go/callgraph/rta` for Go repos
- [ ] Produces type-aware, compiler-accurate call resolution
- [ ] Merge with tree-sitter call graph (RTA for Go files, tree-sitter for others)
- [ ] Resolves interface dispatch, method sets, embedded types

### 11.3 Compound tools + graceful degradation
- [ ] `explore` — combines `file_parse` + `symbol_search` + `dep_graph` for area overview
- [ ] `understand` — combines `call_trace` + `code_graph` + complexity for symbol deep-dive
- [ ] `prepare_change` — combines `impact_analysis` + `dead_code` for pre-change assessment
- [ ] Ambiguity handling: when symbol name matches multiple, list top matches with disambiguation hints (from CodeMCP `UnderstandAmbiguity`)
- [ ] Graceful tier degradation (from CodeMCP): each tool has `MinimumBackend` (tree-sitter / SCIP) + `Fallback` flag. Tools still work at lower precision when SCIP unavailable, instead of failing.
- [ ] Progressive tool disclosure: start with 8 core tools, reveal advanced on request

**Ref**: [CodeMCP](https://github.com/SimplyLiz/CodeMCP) — `explore`, `understand`, `prepareChange`, `expandToolset`; 3-tier system (Basic/Enhanced/Full) with `tier.Detector` + preset switching; [CodeCompass](https://arxiv.org/abs/2602.20048) — agents don't use tools they don't understand, compound tools improve discoverability.

**Deliverable**: Compiler-accurate analysis for Go, compound tools for reduced round-trips.

---

## Dependencies Between Phases

```
Phase 1 (Foundation) ✅ ──→ Phase 2 (Structure) ✅ ──→ Phase 3 (Comparison) ✅
                              2.1 Languages ✅              │
                              2.2 Cleaning ✅                ▼
                              2.3 Analysis ✅   Phase 4 (Advanced) ✅
                              2.3a Noise ✅       4.1 Call trace ✅
                              2.4 Caching ✅      4.2 Code graph ✅
                                                  4.3 Cross-language ✅
                                                        │
                                        ┌───────────────┤
                                        ▼               ▼
                              Phase 5 (Migration) ✅   Phase 6 (Analysis Quality)
                                                        6.1-6.2 Prompt format
                                                        6.3-6.5 Smart ranking
                                                        6.6 Intent classification
                                                        6.7-6.8 Output structure
                                                        6.9 Annotations
                                                              │
                                        ┌─────────────────────┼──────────────────┐
                                        ▼                     ▼                  ▼
                              Phase 7 (Graph Enrich)  Phase 8 (AST Diff)  Phase 9 (Health)
                                7.1 Schema inject       8.1 smacker/gum     9.1 Complexity
                                7.2 IMPORTS             8.2 Edit scripts    9.2 Blast radius
                                7.3 INHERITS            8.3 Visualization   9.3 Dead code
                                7.4 Incremental                                  │
                                      │                                          ▼
                                      ▼                               Phase 10 (Semantic)
                              Phase 11 (Type-Aware)                     10.1 Embeddings
                                11.1 SCIP                               10.2 Search tool
                                11.2 Go-native RTA                      10.3 Graph-enhanced
                                11.3 Compound tools
```

**Completed**: Phase 1, 2, 3, 4 (4.1+4.2+4.3), 5, 6.
**Next**: Phase 7 (graph enrichment — schema injection, IMPORTS, INHERITS, incremental indexing, identifier-level ranking).
**Independent**: Phases 7, 8, 9 can run in parallel after Phase 6.
**Depends on 7+9**: Phase 11 builds on enriched graph + impact analysis.
**Depends on 6**: Phase 10 builds on PageRank from Phase 6.5.
**Depends on 7.5**: Phase 10.3 graph-enhanced search benefits from identifier-level reference graph.

## Releases

| Tag | Commit | What |
|-----|--------|------|
| v1.0.0 | `c4a5c55` | Phase 1: Foundation — 5 MCP tools, Go/Python/TypeScript parsing |
| v1.1.0 | `5e75bc5` | Phase 2.1: 6 new languages (Rust, Java, C, C++, Ruby, C#) |
| v1.2.0 | `cb0fc1f` | Phase 2.3a: Noise reduction, test filtering, symbol limits, dep_graph fixes |
| v1.3.0 | `24613ba` | Phase 2.2: Render modes (signatures, skeleton, focused) for `repo_analyze` |
| v1.3.1 | `72e8617` | Fix render bugs: dangling braces, nested symbols, validation |
| v1.4.0 | `a99d14d` | Phase 2.3+2.4: Multi-level analysis (depth) + LRU caching |
| v1.5.0 | `4e471f0` | Phase 3: Comparison Engine — `code_compare` with structural diff + LLM analysis |
| v1.5.1 | `eb70fe0` | Fix 6 bugs found during practical testing of `code_compare` |
| v1.6.0 | `36f2144` | Phase 4.1: Call chain tracing — `call_trace` with bidirectional BFS + LLM narrative |
| v1.7.0 | `07e8907` | Phase 5: go-search migration — `repo_search`, `repo_analyze` quick/issues modes, retry, Redis L2, metrics |
| v1.8.0 | `127fd2d` | Phase 4.2: Code graph — `code_graph` with Apache AGE, NL→Cypher templates + LLM freeform, lazy indexing |
| v1.9.0 | `b06e340` | Phase 4.3: Cross-language analysis — polyglot detection, HTTP route extraction (7 langs), Layer/Route graph, 14 Cypher templates |

## Technical Debt Watch

- [ ] tree-sitter grammar version pinning (test after upgrades)
- [ ] CGO cross-compilation for ARM64 Docker builds
- [ ] Memory usage profiling for large repos (10K+ files)
- [x] Cache eviction strategy for long-running server (LRU + TTL in internal/cache)
- [ ] Rate limiting for GitHub API calls
- [x] MCP SDK v1.4.0 output schema compatibility (fixed: Out type must be `any`, not struct — otherwise `structuredContent: {}` overrides `content`)
- [x] jsonschema tag format (fixed: jsonschema_description instead of jsonschema:"description=")
- [x] Consistent versioning scheme (re-tagged: v1.0.0 → v1.4.0 in correct order)
