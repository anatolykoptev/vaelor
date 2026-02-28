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

### 4.2 Code graph (optional, if needed)
- [ ] Store symbols + relationships in Apache AGE (already in stack)
- [ ] Schema: File/Function/Type + CALLS/IMPORTS/CONTAINS
- [ ] NL → Cypher query translation via LLM
- [ ] "Who calls function X?" "What depends on package Y?"

### 4.3 Cross-language analysis
- [ ] Detect polyglot repos (Go backend + TS frontend)
- [ ] Map API boundaries (Go HTTP handlers ↔ TS fetch calls)
- [ ] Unified dependency graph across languages

**Deliverable**: Deep analysis capabilities, call chain tracing, optional graph storage.

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

## Dependencies Between Phases

```
Phase 1 (Foundation) ✅ ──→ Phase 2 (Structure) ✅ ──→ Phase 3 (Comparison) ✅
                              2.1 Languages ✅              │
                              2.2 Cleaning ✅                ▼
                              2.3 Analysis ✅   Phase 4 (Advanced) ←──┘
                              2.3a Noise ✅            │
                              2.4 Caching ✅           ▼
                                              Phase 5 (Migration)
```

Phase 1 complete. Phase 2 complete. Phase 3 complete. Phase 4.1 complete. Phase 5 complete.
Phase 4.2/4.3 remain.

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

## Technical Debt Watch

- [ ] tree-sitter grammar version pinning (test after upgrades)
- [ ] CGO cross-compilation for ARM64 Docker builds
- [ ] Memory usage profiling for large repos (10K+ files)
- [x] Cache eviction strategy for long-running server (LRU + TTL in internal/cache)
- [ ] Rate limiting for GitHub API calls
- [x] MCP SDK v1.4.0 output schema compatibility (fixed: Out type must be `any`, not struct — otherwise `structuredContent: {}` overrides `content`)
- [x] jsonschema tag format (fixed: jsonschema_description instead of jsonschema:"description=")
- [x] Consistent versioning scheme (re-tagged: v1.0.0 → v1.4.0 in correct order)
