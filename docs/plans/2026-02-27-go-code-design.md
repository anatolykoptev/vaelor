# go-code Design Document

**Date**: 2026-02-27
**Status**: Approved
**Author**: krolik + Claude

## Problem Statement

`github_repo_analyze` in go-search has limitations:
1. Loses files/folders — gitingest filters are too aggressive, misses content
2. Flat analysis — one prompt, one answer. No understanding of code structure or dependencies
3. No comparison — cannot compare implementations between repositories
4. No smart cleaning — removes comments but doesn't extract the essence (interfaces, API surface, flow)

## Goals

Build a standalone MCP server (`go-code`) that provides deep code intelligence:
- **Parse**: Extract structured symbols (functions, types, imports) from any language via tree-sitter
- **Understand**: Build dependency graphs showing what calls what, what imports what
- **Compare**: Structurally compare 2-3 repos/modules side-by-side
- **Analyze**: Answer natural language questions about code with full structural context
- **Clean**: Strip noise, preserve signal — show LLM only what matters

## Non-Goals

- IDE integration (LSP server) — we're MCP, not LSP
- Real-time file watching — analyze on demand
- Code generation/modification — read-only analysis
- Plagiarism detection — similarity for learning, not enforcement

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   MCP Server                     │
│  cmd/go-code/ — HTTP :8897 + stdio transport     │
│                                                   │
│  Tools: repo_analyze, file_parse, code_compare,  │
│         dep_graph, symbol_search                  │
└───────────┬───────────────────────────────────────┘
            │
    ┌───────┴───────┐
    │  internal/     │
    │  analyze/      │ ← orchestration layer
    └───┬───┬───┬───┘
        │   │   │
   ┌────┘   │   └────┐
   │        │        │
┌──┴──┐ ┌──┴──┐ ┌──┴────┐
│ingest│ │parser│ │compare│
│      │ │      │ │       │
│clone │ │tree- │ │struct │
│walk  │ │sitter│ │diff   │
│filter│ │AST   │ │align  │
└──────┘ └──┬───┘ └───────┘
            │
        ┌───┴───┐
        │queries/│
        │go.scm  │
        │py.scm  │
        │ts.scm  │
        └────────┘

Supporting packages:
  internal/clean/    — smart code cleaning for LLM context
  internal/github/   — GitHub API client (meta, README, code search)
  internal/llm/      — LLM client via CLIProxyAPI
```

### Dependency Direction

```
cmd/go-code/tool_*.go
    → internal/analyze/   (orchestration)
        → internal/ingest/   (clone + walk)
        → internal/parser/   (tree-sitter AST)
        → internal/clean/    (noise removal)
        → internal/compare/  (structural diff)
        → internal/github/   (API client)
        → internal/llm/      (LLM calls)
```

No circular dependencies. Each internal package is self-contained.
`ingest` knows nothing about `parser`. `parser` knows nothing about `llm`.
`analyze` is the glue that connects them all.

## MCP Tools

### 1. `repo_analyze`

Deep analysis of a repository. Replaces current `github_repo_analyze` in go-search.

**Input**:
```json
{
  "repo": "owner/repo",       // GitHub slug or URL
  "path": "/local/path",      // OR local filesystem path
  "query": "how does auth work?",
  "pattern": "*.go",          // optional file filter
  "language": "ru",           // optional response language
  "max_depth": 3,             // analysis depth (1=overview, 2=modules, 3=functions)
  "include_graph": true       // include dependency graph
}
```

**Flow**:
1. Clone/access repo → `ingest.IngestRepo()` — walk ALL files, respect gitignore
2. Parse each file → `parser.ParseFile()` — extract symbols via tree-sitter
3. Build dependency graph → `analyze.BuildDepGraph()` — imports + calls
4. Clean code for context → `clean.CleanSource()` — strip noise, keep API surface
5. Rank by relevance → git change frequency + query-symbol matching
6. LLM analysis → send structured context + query → get answer

**Output**:
```json
{
  "repo": "owner/repo",
  "summary": "...",
  "modules": [
    {
      "file_path": "internal/auth/handler.go",
      "symbols": ["HandleLogin", "ValidateToken", "AuthMiddleware"],
      "description": "...",
      "code_snippet": "func HandleLogin(w http.ResponseWriter, r *http.Request) { ... }"
    }
  ],
  "dep_graph": "A -> B -> C",
  "stats": {"files": 42, "functions": 156, "types": 23}
}
```

### 2. `file_parse`

Parse a single file with tree-sitter. Returns symbol table or raw AST.

**Input**: `{"path": "/path/to/file.go", "include_body": true}`
**Output**: List of Symbol objects with name, kind, signature, line numbers, body.

### 3. `code_compare`

Compare 2-3 repositories structurally.

**Input**:
```json
{
  "repos": [
    {"repo": "gin-gonic/gin"},
    {"repo": "labstack/echo"}
  ],
  "query": "middleware implementation",
  "focus": "internal/middleware/"
}
```

**Flow**:
1. Ingest + parse both repos in parallel
2. Match symbols by purpose (function names, signatures, patterns)
3. Align matching modules side by side
4. Calculate metrics: LOC, complexity, dependency count
5. LLM: compare architectures, patterns, trade-offs

**Output**:
```json
{
  "comparison": [
    {
      "aspect": "Middleware chaining",
      "repo_a": {"approach": "...", "code": "...", "pros": "..."},
      "repo_b": {"approach": "...", "code": "...", "pros": "..."},
      "verdict": "..."
    }
  ],
  "metrics": {"repo_a": {...}, "repo_b": {...}},
  "summary": "..."
}
```

### 4. `dep_graph`

Build and visualize the dependency graph.

**Input**: `{"repo": "owner/repo", "format": "mermaid"}` (or `dot`, `json`)
**Output**: Graph in requested format. Shows packages → imports, functions → calls.

### 5. `symbol_search`

Search for symbols across a repo.

**Input**: `{"repo": "owner/repo", "name": "Handle*", "kind": "function"}`
**Output**: Matching symbols with file paths, signatures, line numbers.

## Key Technical Decisions

### 1. tree-sitter binding: `smacker/go-tree-sitter`

- 20+ grammars bundled → no separate `go get` per language
- More production-tested than the official binding
- Query API with S-expression patterns
- Requires CGO_ENABLED=1

### 2. Language interface pattern

Following sourcegraph/doctree:

```go
type LanguageHandler interface {
    Language() string
    Extensions() []string
    Parse(source []byte) (*ParseResult, error)
    ExtractSymbols(tree *sitter.Tree, source []byte) []*Symbol
    ExtractImports(tree *sitter.Tree, source []byte) []string
}
```

Each language embeds its `.scm` query file via `//go:embed`.
Register handlers in a map keyed by file extension.

### 3. Parallel processing

```go
g, ctx := errgroup.WithContext(ctx)
sem := make(chan struct{}, runtime.NumCPU())
for _, f := range files {
    f := f
    g.Go(func() error {
        sem <- struct{}{}
        defer func() { <-sem }()
        return parseFile(ctx, f)
    })
}
return g.Wait()
```

### 4. Parsed tree caching

Cache parsed ASTs by `(filePath, modTime, language)` key.
L1: in-memory `sync.Map` with TTL (5 min).
Avoids re-parsing the same file on repeated queries.

### 5. LLM context strategy

Instead of dumping all code into one prompt:

**Level 1 — Overview** (cheap, fast):
- File tree + symbol signatures only
- "What does this repo do?"

**Level 2 — Module** (medium):
- Selected file contents, cleaned
- Dependency graph subset
- "How does auth work?"

**Level 3 — Deep** (expensive, thorough):
- Full function bodies of relevant code
- Call chain tracing
- Cross-file references
- "What happens when a user logs in, step by step?"

### 6. Comparison algorithm

Inspired by difftastic (hash-before-compare) and GumTree (edit script):

1. Parse both repos → symbol tables
2. Hash each symbol by (kind, name_normalized, param_count)
3. Match: identical hashes → direct match
4. Fuzzy: similar names + same kind → candidate match
5. Align matched symbols side-by-side
6. LLM: compare aligned pairs with structural context

### 7. Smart cleaning

Not just "remove comments". Three strategies:

- **Signatures only**: Keep function/type signatures, drop bodies. For overview.
- **Skeleton**: Keep structure (if/for/switch), replace bodies with `...`. For flow understanding.
- **Focused**: Keep full body only for query-relevant symbols. For deep analysis.

## Infrastructure

- **Port**: 8897
- **Docker**: `golang:1.24-alpine` builder (CGO for tree-sitter) → `alpine:3.21` runtime
- **LLM**: CLIProxyAPI at :8317, gemini-2.5-flash default
- **GitHub**: GITHUB_TOKEN for higher rate limits (5000 vs 60 req/hr)
- **Workspace**: `/tmp/go-code-workspace` for temporary clones, semaphore-limited
- **Cache**: In-memory only (no Redis dependency initially). Add L2 Redis later if needed.

## Security Considerations

- Clone semaphore (max 3) prevents disk exhaustion
- Workspace cleanup on shutdown and periodic pruning
- `GIT_TERMINAL_PROMPT=0` prevents interactive auth prompts
- Secret scanning: skip files matching `.env`, `credentials.*`, private keys
- Token stripped from error messages
- Symlinks skipped during walk (prevents directory traversal)
- Max file size (512KB) and max repo size (200MB) limits

## Testing Strategy

- **Unit tests**: parser queries against known source files, each language
- **Integration tests**: full ingest → parse → analyze pipeline on a small test repo
- **Comparison tests**: known diffs between two versions of same repo
- **Benchmark**: parse time per file, per language, at various file sizes
