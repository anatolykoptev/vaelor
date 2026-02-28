# go-code Implementation Roadmap

## Phase 1: Foundation — Parse & Analyze (MVP)

**Goal**: Replace `github_repo_analyze` with a better version.
Single tool (`repo_analyze`) that works better than the current one.

### 1.1 tree-sitter integration
- [ ] Add `smacker/go-tree-sitter` dependency
- [ ] Implement `LanguageHandler` interface
- [ ] Go handler with `go.scm` queries — functions, methods, types, imports, consts
- [ ] Python handler with `python.scm` queries
- [ ] TypeScript/JavaScript handler with `typescript.scm` queries
- [ ] Unit tests: parse known Go/Python/TS files, verify symbol extraction

### 1.2 Improved ingestion
- [ ] Port gitingest core: clone, walk, filter, security scanning
- [ ] Fix file loss: increase limits, smarter filtering, configurable depth
- [ ] Git change frequency for relevance ranking
- [ ] File tree rendering
- [ ] Integration test: ingest a real repo, verify no files lost

### 1.3 Smart cleaning
- [ ] Signatures-only mode: extract API surface without bodies
- [ ] Skeleton mode: structure with `...` placeholders
- [ ] Focused mode: full bodies for relevant symbols, signatures for rest
- [ ] Tests for each cleaning mode

### 1.4 LLM analysis
- [ ] LLM client via CLIProxyAPI (OpenAI-compatible)
- [ ] Prompts for repo analysis (overview, module-level, deep)
- [ ] Multi-level analysis: overview → zoom into relevant modules → deep dive
- [ ] JSON output parsing with fallback

### 1.5 `repo_analyze` tool
- [ ] Wire everything: ingest → parse → clean → LLM → output
- [ ] Support GitHub repos and local paths
- [ ] Caching: in-memory by (repo, query) hash
- [ ] Health endpoint + metrics
- [ ] Docker build and deploy

**Deliverable**: `repo_analyze` MCP tool on :8897, demonstrably better than go-search's version.

---

## Phase 2: Structure — Parse & Graph

**Goal**: Expose code structure without LLM. Fast, deterministic tools.

### 2.1 `file_parse` tool
- [ ] Parse single file → symbol table
- [ ] Option: include body, include imports
- [ ] Option: raw AST output (for debugging)

### 2.2 `symbol_search` tool
- [ ] Index all symbols in a repo
- [ ] Wildcard/regex search by name
- [ ] Filter by kind (function, type, interface)
- [ ] Return: file path, line number, signature

### 2.3 `dep_graph` tool
- [ ] Build import graph from parsed files
- [ ] Build call graph from function references
- [ ] Output formats: Mermaid, DOT (Graphviz), JSON
- [ ] Visualization: package-level and function-level views

### 2.4 Additional languages
- [ ] Rust handler + queries
- [ ] Java handler + queries
- [ ] C/C++ handler + queries
- [ ] Ruby handler + queries

**Deliverable**: 4 working MCP tools. Code structure analysis without LLM dependency.

---

## Phase 3: Comparison Engine

**Goal**: Compare implementations between repositories.

### 3.1 Structural diff
- [ ] Symbol-level matching by (kind, name, signature hash)
- [ ] Fuzzy matching for renamed/similar functions
- [ ] Side-by-side alignment of matched symbols
- [ ] Metrics: LOC, complexity, dependency count per repo

### 3.2 `code_compare` tool
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
- [ ] Update Claude MCP config: add go-code, keep go-search
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
Phase 1 (Foundation) ──→ Phase 2 (Structure) ──→ Phase 3 (Comparison)
                                                        │
                                                        ▼
                         Phase 4 (Advanced) ←───────────┘
                                │
                                ▼
                         Phase 5 (Migration)
```

Phase 1 is standalone MVP. Each subsequent phase builds on the previous.
Phase 5 (migration) should only happen after Phase 3 proves go-code is better.

## Technical Debt Watch

- [ ] tree-sitter grammar version pinning (test after upgrades)
- [ ] CGO cross-compilation for ARM64 Docker builds
- [ ] Memory usage profiling for large repos (10K+ files)
- [ ] Cache eviction strategy for long-running server
- [ ] Rate limiting for GitHub API calls
