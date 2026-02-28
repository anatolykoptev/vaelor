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
- [ ] Multi-level analysis: overview → zoom → deep dive (deferred to Phase 2)
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

### 2.3 Multi-level analysis
- [ ] Level 1 (overview): file tree + symbol signatures only
- [ ] Level 2 (module): selected files + dependency graph subset
- [ ] Level 3 (deep): full function bodies + call chain tracing

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

### 2.4 Caching & performance
- [ ] In-memory cache by (repo, query) hash with TTL
- [ ] Parsed AST cache by (filePath, modTime) key
- [ ] Git change frequency for file relevance ranking

**Deliverable**: Better analysis quality, more languages, faster repeated queries.

---

## Phase 3: Comparison Engine

**Goal**: Compare implementations between repositories.

### 3.1 Structural diff
- [ ] Symbol-level matching by (kind, name, signature hash)
- [ ] Fuzzy matching for renamed/similar functions
- [ ] Side-by-side alignment of matched symbols
- [ ] Metrics: LOC, complexity, dependency count per repo

### 3.2 `code_compare` tool (wire the stub)
- [ ] Input: 2-3 repos + query/focus area
- [ ] Parallel ingest + parse
- [ ] Structural alignment
- [ ] LLM: compare aligned code, produce structured report
- [ ] Output: aspect-by-aspect comparison with code snippets

### 3.3 Module-level comparison
- [ ] Compare specific directories/packages between repos
- [ ] "How does repo-A implement auth vs repo-B?"
- [ ] Focus on: architecture, patterns, error handling, testing approach

**Deliverable**: `code_compare` tool that can meaningfully compare two implementations.

---

## Phase 4: Advanced Analysis

**Goal**: Deeper understanding of code.

### 4.1 Call chain tracing
- [ ] "What happens when function X is called?" — trace the full execution path
- [ ] Cross-file, cross-package call chain resolution
- [ ] For Go: enhance with `golang.org/x/tools/go/callgraph/rta` for precision

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

## Phase 5: go-search Migration

**Goal**: Remove code tools from go-search, point to go-code.

### 5.1 Migration
- [ ] Verify go-code covers all github_repo_analyze modes
- [ ] Verify go-code covers github_repo_search functionality
- [x] Update Claude MCP config: add go-code ✅ (done in Phase 1)
- [ ] Remove `tool_github_repo_analyze.go` from go-search
- [ ] Remove `tool_github_repo_search.go` from go-search
- [ ] Remove `internal/gitingest/` from go-search
- [ ] Remove `sources/github.go` code-specific functions from go-search
- [ ] Update go-search CLAUDE.md and tool count

### 5.2 Cleanup
- [ ] Remove dead code from go-search
- [ ] Update MEMORY.md with new tool locations
- [ ] Update agent configurations that reference go-search code tools

**Deliverable**: Clean separation. go-search = web search. go-code = code intelligence.

---

## Dependencies Between Phases

```
Phase 1 (Foundation) ✅ ──→ Phase 2 (Structure) ──→ Phase 3 (Comparison)
                              2.1 Languages ✅            │
                              2.2 Cleaning ✅              ▼
                              2.3 Analysis    Phase 4 (Advanced) ←──┘
                              2.4 Caching            │
                                                     ▼
                                              Phase 5 (Migration)
```

Phase 1 complete. Phase 2.1 (languages), 2.2 (cleaning, v1.3.0–v1.3.1), 2.3a (noise reduction, v0.2.0) complete.
Phase 2.3–2.4 can proceed independently of each other.
Phase 3 is now unblocked (required Phase 2.2).
Phase 5 (migration) should only happen after Phase 3 proves go-code is better.

## Releases

| Tag | Commit | What |
|-----|--------|------|
| v0.2.0 | `cb0fc1f` | Noise reduction: testdata/test filtering, symbol limits, dep_graph fixes, parser fixes |
| v1.3.0 | `24613ba` | Render modes: signatures-only, skeleton, focused for `repo_analyze` |
| v1.3.1 | `72e8617` | Render bug fixes: dangling braces, nested symbols, validation |

Note: versioning is inconsistent (v0.2.0 was tagged after v1.3.x but on an earlier commit). Next release should continue from v1.4.0+.

## Technical Debt Watch

- [ ] tree-sitter grammar version pinning (test after upgrades)
- [ ] CGO cross-compilation for ARM64 Docker builds
- [ ] Memory usage profiling for large repos (10K+ files)
- [ ] Cache eviction strategy for long-running server
- [ ] Rate limiting for GitHub API calls
- [x] MCP SDK v1.4.0 output schema compatibility (fixed: Out type must be `any`, not struct — otherwise `structuredContent: {}` overrides `content`)
- [x] jsonschema tag format (fixed: jsonschema_description instead of jsonschema:"description=")
- [ ] Consistent versioning scheme (v0.2.0 was tagged out of order after v1.3.x)
