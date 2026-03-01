# Competitors & Prior Art

Research conducted 2026-02-27, updated 2026-02-28 (P2 gaps closed). Analysis of existing solutions in code intelligence,
AST parsing, repository analysis, and code comparison.

## Tree-sitter Go Bindings

| Library | Stars | CGo | Bundled Grammars | Status | Recommendation |
|---------|-------|-----|-----------------|--------|----------------|
| [smacker/go-tree-sitter](https://github.com/smacker/go-tree-sitter) | 539 | Yes | 20+ languages | Mature, slowing | Batteries-included, production-tested |
| [tree-sitter/go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) | 212 | Yes | Modular (separate `go get`) | Official, active | Best for new projects |
| [odvcencio/gotreesitter](https://github.com/odvcencio/gotreesitter) | New | No | 209 grammars | Pure Go, Feb 2026 | Experimental, 11x slower parsing |

**Decision**: Use `smacker/go-tree-sitter` — more production-tested, bundled grammars simplify builds.
Watch `tree-sitter/go-tree-sitter` for a future switch when it matures.

## Code Analysis Tools (tree-sitter based)

### ast-grep — Structural Code Search
- **Repo**: [ast-grep/ast-grep](https://github.com/ast-grep/ast-grep) | 10k+ stars | Rust
- **What**: Polyglot structural search, lint, and rewrite. 20+ languages.
- **Key insight**: Pattern language abstraction — users write `foo($A, $B)` instead of S-expressions.
  This is the UX gold standard for tree-sitter-based search.
- **Useful for us**: Pattern matching UX, YAML rule files, Claude Code plugin exists.
- **Not usable directly**: Rust binary, no Go embedding.

### sourcegraph/doctree — Tree-sitter Indexer in Go (archived)
- **Repo**: [sourcegraph/doctree](https://github.com/sourcegraph/doctree) | 881 stars | Go
- **What**: Multi-language symbol indexer using tree-sitter. Best Go reference architecture.
- **Key pattern** — `Language` interface with embedded queries:

```go
type Language interface {
    Name() schema.Language
    Extensions() []string
    IndexDir(ctx context.Context, dir string) (*schema.Index, error)
}

//go:embed queries.scm
var queries []byte
```

- **Useful for us**: Language interface design, parallel indexing with WaitGroup, modtime caching.
- **Status**: Archived, but patterns are gold.

### DeepSourceCorp/globstar — Static Analysis Toolkit
- **Repo**: [DeepSourceCorp/globstar](https://github.com/DeepSourceCorp/globstar) | 478 stars | Go
- **What**: Two-tier checker API: YAML rules (tree-sitter queries) + full Go API (imports, scope, cross-file).
- **Useful for us**: Dual YAML+Go checker model. Simple rules as YAML, complex as Go code.

### semgrep — Pattern-Based Static Analysis
- **Repo**: [semgrep/semgrep](https://github.com/semgrep/semgrep) | 14,283 stars | OCaml
- **What**: Tree-sitter internally, 30+ languages. Patterns look like real code with `$VAR` wildcards.
- **Useful for us**: Generic AST normalization concept. Not embeddable in Go.

### williamfzc/srctx — Call Graph Extraction
- **Repo**: [williamfzc/srctx](https://github.com/williamfzc/srctx) | 59 stars | Go
- **What**: Function-level call graphs via tree-sitter + SCIP. Diff-aware analysis.
- **Useful for us**: Go-native call graph reference, small and focused.

## Tree-sitter Query Files (.scm)

Best sources for production query files:

| Source | Use |
|--------|-----|
| `tree-sitter/tree-sitter-go` `queries/tags.scm` | Go symbols — used by GitHub for code navigation |
| `nvim-treesitter/nvim-treesitter` (13k stars) | Most comprehensive .scm collection across all languages |
| Language grammar repos (`tree-sitter-{lang}/queries/`) | Official queries per language |

Query file types in each grammar:
- `tags.scm` — symbol navigation (functions, types, refs) — **our primary need**
- `highlights.scm` — syntax coloring (most complete node coverage)
- `locals.scm` — scope/variable resolution
- `injections.scm` — embedded language detection

## Code Comparison & Diff Tools

### difftastic — AST-Level Diff
- **Repo**: [Wilfred/difftastic](https://github.com/Wilfred/difftastic) | 24,230 stars | Rust
- **What**: Tree-sitter → CST → three-phase matching (hash → heuristic → LCS). Side-by-side colored output.
- **Key algorithm**:
  1. Hash-based identical subtree detection (O(n))
  2. Language-specific heuristic matching (signatures, identifiers)
  3. LCS on unmatched children for fine-grained alignment
- **Key insight**: `NodeHash` for O(1) subtree equality checks before expensive traversal.
- **Limitation**: Rust-only, visual output only, no structured data format.

### GumTree — Edit Script Generation
- **Repo**: [GumTreeDiff/gumtree](https://github.com/GumTreeDiff/gumtree) | 1,280 stars | Java
- **What**: Produces structured edit scripts: Insert, Delete, Update, Move operations.
  Academic gold standard (ICSE 2014, TSE 2023 papers).
- **Key algorithm**: GreedySubtreeMatcher → BottomUpMatcher → TopDownMatcher pipeline.
- **Key data structure**: `MappingStore` — bidirectional map between source/destination nodes.
  `CandidateMultiset` — groups nodes by hash+size for O(1) candidate lookup.
- **Useful for us**: Edit script as output format, matching pipeline architecture.
- **Limitation**: Java only, no Go port exists.

### codinuum/diffast — Normalized AST Diff
- **Repo**: [codinuum/diffast](https://github.com/codinuum/diffast) | ~200 stars | OCaml
- **Key insight**: Normalizes ALL languages into a common `Ast.t` type. Enables cross-language
  structural similarity. Exports diffs as facts in XML/RDF.

## Repo-to-LLM Ingestion Tools

### repomix
- **Repo**: [yamadashy/repomix](https://github.com/yamadashy/repomix) | 22,131 stars | TypeScript
- **What**: Pack entire repo into LLM-friendly format (XML, Markdown, JSON, plain).
- **Key features**: Token counting (tiktoken), output splitting for large repos, MCP server mode.
- **Key insight**: XML as default — LLMs parse structured tags reliably.

### code2prompt
- **Repo**: [mufeedvh/code2prompt](https://github.com/mufeedvh/code2prompt) | 7,169 stars | Rust
- **What**: Codebase → structured prompt. Handlebars templates, token counting, PyO3 Python SDK.
- **Key pattern**: Two-layer filtering — WalkBuilder (gitignore) + FileFilter (user globs).

### yek
- **Repo**: [mohsen1/yek](https://github.com/mohsen1/yek) | 2,430 stars | Rust
- **Key differentiator**: Git history → file importance ranking. Recently changed = higher priority.
  Can cap output to N tokens by priority. Already in our gitingest via `GitChangeFrequency`.

### Go-native alternatives
- **CodeWeaver** (721 stars, Go) — Go equivalent of repomix. `filepath.WalkDir` + tree rendering.
- **shotgun_code** (1,989 stars, Go) — XML-tagged output for LLM consumption.

## Code Intelligence Platforms

### Sourcegraph SCIP — Code Intelligence Protocol
- **Repo**: [sourcegraph/scip](https://github.com/sourcegraph/scip) | 521 stars | Go
- **What**: Replaces LSIF. Language-agnostic Protobuf schema for indexing code symbols.
  Human-readable string IDs. 26 SymbolKind values. Definition vs Reference tracking.
- **Indexers**: scip-go, scip-typescript, scip-java, scip-python, scip-rust, scip-clang, scip-ruby.
- **Useful for us**: SymbolKind enum as output standard. Interop with Sourcegraph ecosystem.

### Serena — LSP-backed MCP Server
- **Repo**: [oraios/serena](https://github.com/oraios/serena) | 20,742 stars | Python
- **What**: Symbol-level MCP tools backed by real language servers (gopls, pyright, rust-analyzer).
- **Key tools**: `find_symbol`, `find_referencing_symbols`, `get_symbols_overview`,
  `insert_after_symbol`, `rename_symbol`.
- **Key insight**: "The agent no longer needs to read entire files." Navigate by symbol, not by file.
- **Limitation**: Python, requires running LSP servers.

### Code Graph RAG
- **Repo**: [vitali87/code-graph-rag](https://github.com/vitali87/code-graph-rag) | 1,970 stars | Python
- **What**: Tree-sitter → Memgraph graph DB → NL query → LLM generates Cypher → code retrieval.
- **Graph schema**: `File/Function/Class/Module + CALLS/INHERITS/CONTAINS/IMPORTS`.
- **Key insight**: NL → Cypher translation with schema grounding in the prompt.
- **Applicable**: We have Apache AGE (Cypher-compatible) already in our stack.

### CodeGraphContext
- **Repo**: [CodeGraphContext/CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext) | 886 stars
- **What**: Tree-sitter + graph DB for MCP tools. Clean minimal graph schema.
- **Useful for us**: Schema model to adopt.

## MCP Server Patterns (Go)

### mark3labs/mcp-go
- **Repo**: [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) | 8,239 stars | Go
- **What**: De facto Go MCP SDK. Used by GitHub's official MCP server.
- **Pattern**: `NewTool` + functional options + `AddTool` + `ServeHTTP`.

### github/github-mcp-server
- **Repo**: [github/github-mcp-server](https://github.com/github/github-mcp-server) | 27,304 stars | Go
- **What**: Production Go MCP server. Middleware pattern, toolset grouping, context propagation.
- **Useful for us**: Production patterns for Go MCP servers.

## Go Code Analysis (Native)

| Package | Purpose | Notes |
|---------|---------|-------|
| `golang.org/x/tools/go/callgraph/rta` | RTA call graph | ~2s on medium projects, precise |
| `golang.org/x/tools/go/packages` | Go package loading with type info | Required for SSA |
| `golang.org/x/tools/go/ssa` | SSA form construction | Needed for RTA |
| `go/ast` | Go AST parsing | Full type info via `go/types` |

For Go repos, native `golang.org/x/tools` produces higher-quality results than tree-sitter.
Use tree-sitter for all other languages, Go-native tools as a precision enhancement for Go.

## Code Metrics

### boyter/scc
- **Repo**: [boyter/scc](https://github.com/boyter/scc) | 8,071 stars | Go
- **What**: LOC, blank, comment, cyclomatic complexity per file. Duplicate detection.
- **Useful for us**: Quantitative repo fingerprinting for comparison.

### gabotechs/dep-tree
- **Repo**: [gabotechs/dep-tree](https://github.com/gabotechs/dep-tree) | 1,696 stars | Go
- **What**: Cross-language dependency graph visualization.
- **Useful for us**: Reference for dep graph rendering.

## AI Coding Assistants — Context & Ranking (updated 2026-02-28)

How leading AI coding tools build LLM context and rank code for relevance.

### Aider — RepoMap with PageRank
- **Repo**: [Aider-AI/aider](https://github.com/Aider-AI/aider) | ~22K stars | Python
- **Key file**: `aider/repomap.py`
- **Approach**: tree-sitter tag extraction → file-to-file reference graph → Personalized PageRank (networkx)
- **Skeleton format**: `│func Foo()` + `⋮...` for omitted code — explicit truncation signal for LLM
- **Dynamic budget**: 8x expansion when no specific files mentioned
- **Personalization**: chat files weight=10, mentioned identifiers weight=1 — proximity-based ranking
- **Go port exists**: [codeberg.org/MadsRC/aigent](https://codeberg.org/MadsRC/aigent) `internal/repomap`

**Deep dive (2026-02-28):**

Architecture of RepoMap pipeline:
1. **Tag extraction** (`get_tags_raw`): tree-sitter `.scm` queries extract `Tag(rel_fname, name, kind="def"|"ref", line)` per file
2. **Pygments fallback**: if tree-sitter finds defs but no refs → fallback to `pygments` Token.Name extraction. Guarantees graph edges for poorly-covered languages
3. **Graph construction** (`get_ranked_tags`): NetworkX MultiDiGraph, files as nodes:
   - Self-edges: `(definer → definer, weight=0.1)` for each defined identifier
   - Reference edges: `(referrer → definer, weight=mul/len(definers))` per identifier reference
   - `mul` multiplier: ×10 for mentioned identifiers, ×10 for long camelCase/snake_case (≥8 chars), ×0 for `_` prefixed
4. **Personalized PageRank** (`nx.pagerank(alpha=0.85, personalization=...)`): personalization vector boosts chat files + mentioned files + files matching mentioned identifiers
5. **Token-budgeted output**: iterate ranked files, collect tags, stop at `max_map_tokens`
6. **Rendering** (`render_tree`, `to_tree`): hierarchical path + lines-of-interest format
7. **Caching**: `diskcache` (SQLite) with mtime invalidation, fallback to in-memory dict on corruption

Strategy pattern for edit formats (6 coders):
- `diff` (SEARCH/REPLACE blocks) — most common, robust `flexible_search_and_replace` with fuzzy matching
- `whole` — entire file content
- `patch` — custom V4A diff format
- `udiff` — standard unified diff
- `func` — OpenAI function calling (`write_file`)
- `diff-fenced` — variant with filename inside fence

**Key insight for go-code**: Our Phase 6.5 PageRank uses file-level import edges only. Aider builds identifier-level reference edges (much denser graph). Upgrading to identifier-level edges in Phase 7.5 would significantly improve ranking quality.

### Continue.dev — Hybrid BM25 + Embeddings
- **Repo**: [continuedev/continue](https://github.com/continuedev/continue) | ~25K stars | TypeScript
- **Key files**: `core/src/util/search/BM25.ts`, `core/src/context/providers/CodebaseContextProvider.ts`
- **Pipeline**: keyword search (lunr/BM25) + embedding search → RRF (Reciprocal Rank Fusion) → optional LLM reranking
- **Key insight**: Hybrid BM25+embeddings = -49% failed retrievals; +reranking = -67% (Anthropic Contextual Retrieval)

### Cursor — Merkle Tree + AST Chunking
- **Approach** (from blog, closed source): Merkle tree for incremental diff → AST chunking via tree-sitter (function boundaries) → embeddings per chunk → vector DB (Turbopuffer)
- **Content-hash caching**: unchanged code chunks reuse embeddings. Simhash for teammate index reuse (4h → 21s)
- **Result**: +12.5% accuracy from semantic search

### Sourcegraph Zoekt — BM25F for Code
- **Repo**: [sourcegraph/zoekt](https://github.com/sourcegraph/zoekt) | Go
- **Blog**: [keeping-it-boring-and-relevant-with-bm25f](https://sourcegraph.com/blog/keeping-it-boring-and-relevant-with-bm25f)
- **Fields**: symbol definitions (highest weight) > filename > file content (lowest)
- **Params**: k1=1.2, b=0.75, language-specific stopwords
- **Result**: +20% across all search quality metrics
- **Code tokenization**: `getUserById` → `[get, user, by, id, getUserById]` (both original and camelCase split)

### Sourcegraph — PageRank for Code
- **Blog**: [ranking-in-a-week](https://sourcegraph.com/blog/ranking-in-a-week)
- **Approach**: SCIP-based reference graph (file A references symbol in file B → edge A→B), PageRank via Apache Spark
- **Interpretation**: high PageRank = high code reuse = architecturally important

### Cline — Dynamic Context via Tools
- **Repo**: [cline/cline](https://github.com/cline/cline) | ~40K stars | TypeScript
- **System prompt**: 59K chars, 12K tokens, XML-formatted tool descriptions
- **Context strategy**: no static pre-selection; LLM explores via `list_files`, `search_files`, `list_code_definition_names`
- **Key insight**: for batch analysis (like go-code), pre-ranking is better; for interactive agents, exploration tools are better

## Code Quality & Hotspot Analysis

### panbanda/omen — Hotspot Detection
- **Repo**: [panbanda/omen](https://github.com/panbanda/omen) | Rust
- **Key file**: `src/analyzers/hotspot.rs`
- **Formula**: `hotspot = percentile(churn) × percentile(complexity)` — product of percentile ranks
- **Thresholds**: critical ≥ 0.81, high ≥ 0.64, moderate ≥ 0.36 (both churn and complexity must be ≥ 50th percentile)
- **Churn scoring**: `sum(1.0 + (additions + deletions) / 100.0)` per commit — larger changes weighted more

### Neural Code Retrieval (arxiv 2502.07067)
- **Title**: "Repository-level Code Search with Neural Retrieval Methods" (Feb 2025)
- **Pipeline**: BM25 over commit messages → CodeBERT CommitReranker → CodeBERT CodeReranker
- **Key insight**: commit messages are natural language descriptions of what code does; BM25 on commit messages outperforms BM25 on source code for bug localization
- **Result**: up to 80% improvement in MAP/MRR/P@1 vs BM25 baseline

## Key Patterns to Adopt

1. **Language interface with embedded queries** (from doctree)
2. **Hash-before-compare for AST subtrees** (from difftastic)
3. **Edit script output format** (from GumTree) — Insert/Delete/Update/Move
4. **Graph schema**: File/Function/Class + CALLS/INHERITS/IMPORTS (from CodeGraphContext)
5. **Symbol-level navigation, not file dumps** (from Serena)
6. **Cursor-based AST traversal** (from wrale/mcp-server-tree-sitter) — avoid stack overflow
7. **Token counting for LLM context management** (from repomix/code2prompt)
8. **Git history for file importance ranking** (from yek) — already in our gitingest
9. **XML tags for LLM context** (from Repomix) — `<file path="x" score="N">` beats Markdown headings
10. **BM25F with code fields** (from Zoekt) — symbol×5, path×3, content×1; +20% quality
11. **PageRank on import graph** (from Aider/Sourcegraph) — transitive importance via graph centrality
12. **Response envelope with suggestedNextCalls** (from CodeMCP) — only MCP server with guided tool chaining
13. **Skeleton markers** (from Aider) — `│` prefix + `⋮...` for truncation signals
14. **Hotspot = churn × complexity** (from Omen) — percentile product, not sum
15. **Intent-aware prompts** (research synthesis) — classify query type → select system prompt

## Anti-Patterns to Avoid

1. **Dumping full files into LLM context** — return symbol-level excerpts instead
2. **Line-based diff for structural comparison** — use AST diff
3. **Recursive AST traversal** — use cursor API for large files
4. **Not calling `defer .Close()` on tree-sitter objects** — causes memory leaks
5. **Bundling grammar C source in the project** — use modular `go get` dependencies
6. **Manual AST walking instead of queries** — tree-sitter query engine is faster
7. **NL → Cypher without schema in prompt** — causes hallucinated property names
8. **Re-parsing on every request** — cache parsed trees by `(path, modtime)`
9. **Naive keyword scoring without IDF** — rare terms should score higher than common ones
10. **76 tools** (CodeMCP anti-pattern) — agents don't use 80% of tools; 8-12 is optimal (CodeCompass research)

## MCP Code Intelligence Servers (updated 2026-02-28)

Full landscape analysis: 15+ competing MCP servers for code intelligence.

### Tier 1 (1000+ stars)

| Project | Stars | Lang | Approach | Key Differentiator |
|---------|-------|------|----------|-------------------|
| [Serena](https://github.com/oraios/serena) | 20.8K | Python | LSP-backed, 40+ languages, 30+ tools | True semantic understanding via language servers (rename, references) |
| [kit](https://github.com/cased/kit) | 1.3K | Python | tree-sitter + Chroma + package search | AST pattern search (`grep_ast`) with 3 modes, PR review |
| [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext) | 903 | Python | tree-sitter → FalkorDB/Neo4j | Dual graph backend (FalkorDB Lite for dev, Neo4j for prod), file watching |

### Tier 2 (100-1000 stars)

| Project | Stars | Lang | Approach | Key Differentiator |
|---------|-------|------|----------|-------------------|
| [Axon](https://github.com/harshkedia177/axon) | 384 | Python | 12-phase pipeline → KuzuDB graph | Blast radius with confidence scores, dead code, git change coupling |
| [mcp-server-tree-sitter](https://github.com/wrale/mcp-server-tree-sitter) | 253 | Python | Cursor-based tree-sitter traversal | Parse caching, project registration model, state persistence |
| [Octocode](https://github.com/Muvon/octocode) | 236 | Rust | GraphRAG + optional LSP | Rust performance, AI memory system, smart commits |
| [Axon.MCP.Server](https://github.com/ali-kamali/Axon.MCP.Server) | 151 | Python | 10-service microarchitecture, pgvector | Multi-parser (tree-sitter + Roslyn), 12 tools |

### Tier 3 (< 100 stars, architecturally interesting)

| Project | Stars | Lang | Key Feature |
|---------|-------|------|-------------|
| [code-graph-mcp](https://github.com/entrepeneur4lyf/code-graph-mcp) | 80 | Python | ast-grep backend, 25+ languages, LRU cache (50-90% speedup) |
| [CodeMCP](https://github.com/SimplyLiz/CodeMCP) | 62 | **Go** | **Most relevant competitor.** 76 tools, SCIP + tree-sitter, stable identity system, multi-repo federation, blast radius, dead code, ownership detection |
| [ast-mcp-server](https://github.com/angrysky56/ast-mcp-server) | 30 | Python | AST diff (`diff_ast`), ASG (Abstract Semantic Graph), Neo4j |
| [tree-sitter-mcp](https://github.com/nendotools/tree-sitter-mcp) | 27 | TypeScript | Minimal: 4 focused tools, dual CLI+MCP mode |
| [codeprism](https://github.com/rustic-ai/codeprism) | new | Rust | Universal AST, DashMap incremental indexing, `RoutesTo` edges for polyglot |

### CodeMCP Deep Dive (closest competitor)

**Go-native, 683 files, 80 internal packages.** Most architecturally complex code MCP server analyzed.

**Key architecture (2026-02-28 repo_analyze deep dive):**

- **SCIP as primary backend** (`internal/backends/scip/`): `SCIPAdapter` exposes `GetSymbol`, `FindReferences`, `BuildCallGraph`. Falls back to tree-sitter when SCIP unavailable. For Go, `scip-go` provides type-aware call graphs.
- **3-tier analysis system** (`internal/tier/`):
  - `Basic` (tree-sitter only) → `Enhanced` (+ SCIP index) → `Full` (+ SCIP + telemetry)
  - Each tool has `MinimumTier` + `Fallback` flag (can degrade gracefully)
  - `tier.GetToolsForTier()` / `tier.GetUnavailableTools()` — dynamic tool filtering
  - `tier.Detector` auto-detects available backends
- **Identity system** (`internal/identity/`): Stable SCIP-based symbol IDs (survive minor refactors). `SymbolFingerprint` as content-hash fallback when SCIP unavailable. Alias chains track renames.
- **Fusion ranking** (`internal/query/ranking.go`):
  - `FusionRanker` combines 5 signals: FTS (full-text), PPR (PageRank), Hotspot, Recency, Exact Match
  - Personalized PageRank: seeds = top 10 FTS results + expanded class methods
  - `graph.BuildFromSCIP()` with `DefaultEdgeWeights()` — weighted graph from SCIP index
  - Reranking formula: `0.6 * positionScore + 0.4 * pprScore`
  - Normalization: all signals scaled to [0,1] before weighted combination
- **Impact analysis** (`internal/impact/`):
  - Direct: `SCIP.FindReferences()` → classify into `DirectCaller`, `ImplementsInterface` etc.
  - Transitive: `SCIP.BuildCallGraph()` with MaxDepth + MaxNodes limits
  - Confidence score degrades with distance
  - Blast radius thresholds: low (2 modules / 5 callers), medium (5 / 20), high (above)
  - Risk scoring: visibility × impact count
  - Telemetry enhancement: observed usage data boosts/lowers risk score
- **Progressive tool disclosure**: Presets (core, review, refactor, federation, docs, ops, full). `expandToolset` MCP tool switches active preset + sends `notifications/tools/list_changed`.
- **Ownership** (`internal/ownership/`): CODEOWNERS + git blame.
- **Compound tools** (`internal/query/compound.go`): `explore`, `understand`, `prepareChange` — multi-step wrappers. `understand` handles `UnderstandAmbiguity` by listing top matches with disambiguation hints.
- **Additional packages**: audit, auth, breaking (changes), complexity, compression, coupling, cycles, daemon, deadcode, decisions, diff, docs, envelope, explain, export, extract, federation, hotspots, incremental, index, jobs, modules, scheduler, secrets, streaming, suggest, telemetry, testgap, watcher, webhooks.

**What's genuinely good:**
1. Fusion ranking (5 signals, not just 1-2) — Phase 7.5 should adopt
2. Graceful tier degradation (tool still works, just less precise) — Phase 11 should adopt
3. Blast radius with concrete thresholds (not vague "high/low") — Phase 9.2 should adopt
4. Seed expansion for PPR (class → add methods) — Phase 7.5

**Anti-patterns to avoid:**
1. 683 files / 80 packages for a code analysis tool — overengineered
2. 76+ tools (CodeCompass: agents don't use 80% of tools) — go-code's 8 is closer to optimal
3. Telemetry as a tier requirement — premature for our use case
4. Separate `breaking`, `coupling`, `cycles`, `responsibilities` packages — many of these are simple graph queries, not separate packages

### AST Diff Tools

| Tool | Stars | Lang | Algorithm | Key Insight |
|------|-------|------|-----------|-------------|
| [smacker/gum](https://github.com/smacker/gum) | ~50 | **Go** | GumTree (subtree match → bottom-up Jaccard → Zhang-Shasha → edit script) | **Same author as `smacker/go-tree-sitter`**. `gum/tsitter/` adapter included. Insert/Delete/Update/Move operations. |
| [cedricrupb/code_diff](https://github.com/cedricrupb/code_diff) | small | Python | Fast GumTree reimplementation | Confirms tree-sitter + GumTree works well together |

### Academic Research

| Paper | Key Finding |
|-------|-------------|
| [CodeCompass (arxiv 2602.20048)](https://arxiv.org/abs/2602.20048) | Graph-based navigation: 99.4% task completion vs 76.2% baseline. But 58% of agents with graph access made 0 tool calls — need explicit prompting for graph tools. |
| [MLSA (arxiv 1808.01213)](https://arxiv.org/abs/1808.01213) | Build monolingual call graphs independently, stitch at FFI boundaries. Lightweight polyglot analysis. |
| [CHARON (EuroSP 2025)](https://scnps.co/papers/eurosp25_polyglot_sast.pdf) | Polyglot Property Graphs with bidirectional cross-language edges for SAST. |

### Registries

- [Official MCP servers](https://github.com/modelcontextprotocol/servers) — no dedicated code analysis server. Space is entirely community-driven.
- [awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers) — 7260+ servers cataloged, weak coverage of dedicated code analysis.

## Comparative Feature Matrix

| Feature | go-code | Serena | kit | Axon | CodeMCP |
|---------|---------|--------|-----|------|---------|
| **Language** | Go | Python | Python | Python | Go |
| **Parsing** | tree-sitter | LSP | tree-sitter | tree-sitter | SCIP + tree-sitter |
| **Languages** | 10 | 40+ | 15+ | multi | multi |
| **Call graph** | Yes (BFS) | No | No | Yes | Yes (SCIP) |
| **Graph DB** | Apache AGE | No | No | KuzuDB | In-memory |
| **NL→Cypher** | Yes (schema-injected) | No | No | Yes | No |
| **Code compare** | Yes | No | No | No | No |
| **Impact/blast** | **Yes** | No | No | Yes | Yes |
| **Dead code** | **Yes** (MCP tool + template) | No | No | Yes | Yes |
| **AST diff** | **Yes** (smacker/gum) | No | No | No | No |
| **Complexity** | **Yes** (cyclomatic + hotspots) | No | No | No | Yes |
| **INHERITS/IMPLEMENTS** | **Yes** (Go/Py/Java/TS) | No | No | Yes | Yes |
| **PageRank** | **Yes** (CALLS graph) | No | No | No | Yes |
| **Fallback tokenizer** | **Yes** (8 extra langs) | No | No | No | No |
| **Incremental indexing** | **Yes** (mtime tracking) | No | No | No | No |
| **Semantic search** | No | No | Yes (Chroma) | Yes | No |
| **SCIP backend** | No | No | No | No | Yes |
| **Repo search** | Yes | No | Yes | No | No |
| **Multi-repo** | No | No | No | No | Yes |
| **IMPORTS graph** | **Yes** | No | No | Yes | Yes |

## Gap Analysis — What We Build

No existing tool combines ALL of:
- Go-native implementation (only CodeMCP is also Go)
- Multi-language AST parsing (tree-sitter, 10+ languages + 8 via fallback tokenizer)
- Apache AGE knowledge graph with schema-injected NL-to-Cypher
- Structural code comparison with AST diff (`code_compare` + `smacker/gum` — unique)
- Impact/blast radius analysis with confidence scoring
- IMPORTS edges in graph with full Cypher template support
- INHERITS/IMPLEMENTS edges for type hierarchy (Go embed, Python/Java/TS inheritance)
- Dead code detection MCP tool with false positive filtering and confidence scoring
- Complexity metrics (cyclomatic) and hotspot analysis on Symbol vertices
- PageRank on CALLS graph for symbol importance ranking
- Incremental graph indexing via file mtime tracking
- LLM-powered natural language narratives
- Repo discovery (`repo_search`)
- MCP server interface (10 tools)

This is the gap `go-code` fills.

### Identified Gaps (prioritized)

| Priority | Gap | Effort | Reference | Status |
|----------|-----|--------|-----------|--------|
| ~~P1~~ | ~~Schema injection in freeform Cypher~~ | ~~2h~~ | ~~code-graph-rag~~ | **Done** (v1.9.0) |
| ~~P1~~ | ~~IMPORTS edges (data already parsed)~~ | ~~1d~~ | ~~CodeGraphContext~~ | **Done** (v1.9.0) |
| ~~P1~~ | ~~AST diff via smacker/gum~~ | ~~1d~~ | ~~smacker/gum~~ | **Done** (v1.9.0) |
| ~~P1~~ | ~~Impact/blast radius analysis~~ | ~~1d~~ | ~~Axon, CodeMCP~~ | **Done** (v1.9.0) |
| ~~P2~~ | ~~Dead code detection~~ | ~~1d~~ | ~~Axon, CodeGraphContext~~ | **Done** — `dead_code` MCP tool with confidence scoring |
| ~~P2~~ | ~~Complexity metrics~~ | ~~1d~~ | ~~ast-metrics, CodeMCP~~ | **Done** — cyclomatic + hotspots on Symbol vertices |
| ~~P2~~ | ~~PageRank on CALLS graph~~ | ~~1d~~ | ~~Aider~~ | **Done** — `important_symbols` template |
| ~~P2~~ | ~~Fallback tokenizer~~ | ~~0.5d~~ | ~~Aider~~ | **Done** — 8 extra languages via regex |
| ~~P2~~ | ~~Incremental graph indexing~~ | ~~1d~~ | ~~code-graph-rag~~ | **Done** — mtime tracking infrastructure |
| ~~P2~~ | ~~INHERITS/IMPLEMENTS edges~~ | ~~2d~~ | ~~codeprism~~ | **Done** — Go/Python/Java/TS type hierarchy |
| P3 | Semantic search via embeddings | 3-4d | code-graph-rag, Octocode | |
| P3 | SCIP backend for Go | 3+d | CodeMCP, srctx | |
| P3 | Compound tools | 2d | CodeMCP | |
| ~~P3~~ | ~~Cross-language HTTP boundaries~~ | ~~3d~~ | ~~codeprism, MLSA~~ | **Done** — Route vertices + HANDLES/FETCHES edges + `cross_calls`/`api_routes` templates + matchers for 6 languages |
