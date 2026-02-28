# Competitors & Prior Art

Research conducted 2026-02-27. Analysis of existing solutions in code intelligence,
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

## Key Patterns to Adopt

1. **Language interface with embedded queries** (from doctree)
2. **Hash-before-compare for AST subtrees** (from difftastic)
3. **Edit script output format** (from GumTree) — Insert/Delete/Update/Move
4. **Graph schema**: File/Function/Class + CALLS/INHERITS/IMPORTS (from CodeGraphContext)
5. **Symbol-level navigation, not file dumps** (from Serena)
6. **Cursor-based AST traversal** (from wrale/mcp-server-tree-sitter) — avoid stack overflow
7. **Token counting for LLM context management** (from repomix/code2prompt)
8. **Git history for file importance ranking** (from yek) — already in our gitingest

## Anti-Patterns to Avoid

1. **Dumping full files into LLM context** — return symbol-level excerpts instead
2. **Line-based diff for structural comparison** — use AST diff
3. **Recursive AST traversal** — use cursor API for large files
4. **Not calling `defer .Close()` on tree-sitter objects** — causes memory leaks
5. **Bundling grammar C source in the project** — use modular `go get` dependencies
6. **Manual AST walking instead of queries** — tree-sitter query engine is faster
7. **NL → Cypher without schema in prompt** — causes hallucinated property names
8. **Re-parsing on every request** — cache parsed trees by `(path, modtime)`

## Gap Analysis — What We Build

No existing tool combines ALL of:
- Multi-language AST parsing (tree-sitter)
- Structural code comparison (not just text diff)
- Repository-level analysis with dependency graphs
- LLM-powered natural language Q&A about code
- MCP server interface

This is the gap `go-code` fills.
