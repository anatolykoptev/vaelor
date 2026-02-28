# go-code — Code Intelligence MCP Server

Multi-language code intelligence server powered by tree-sitter AST parsing.
Provides MCP tools for repository analysis, code comparison, dependency graph
visualization, and symbol search across any GitHub or local codebase.

**Supported languages**: Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#

**Go module**: `github.com/anatolykoptev/go-code`
**Port**: 8897
**MCP endpoint**: `http://127.0.0.1:8897/mcp`

## Architecture

```
cmd/go-code/           — MCP server entry point, HTTP/stdio transport
  main.go              — Server setup, signal handling, graceful shutdown
  config.go            — Env var loading with defaults
  register.go          — Tool registration wiring
  tool_*.go            — One file per MCP tool (input type + handler)

internal/
  ingest/              — Repository ingestion (clone, walk, filter)
    ingest.go          — IngestRepo: walk filesystem, detect languages
    clone.go           — CloneRepo: shallow git clone with auth
  parser/              — tree-sitter AST parsing
    parser.go          — ParseFile: extract symbols from source files
    handler.go         — LanguageHandler interface + registry
    handler_go.go      — Go handler
    handler_python.go  — Python handler
    handler_typescript.go — TypeScript/JS handler
    handler_rust.go    — Rust handler
    handler_java.go    — Java handler
    handler_c.go       — C handler
    handler_cpp.go     — C++ handler
    handler_ruby.go    — Ruby handler
    handler_csharp.go  — C# handler
    calls.go           — ExtractCalls: call expression extraction from source
    queries/           — .scm tree-sitter query files per language
      go.scm, python.scm, typescript.scm, rust.scm,
      java.scm, c.scm, cpp.scm, ruby.scm, csharp.scm
      *_calls.scm      — Call expression queries per language
  callgraph/           — Call chain tracing
    graph.go           — BuildCallGraph: name-based resolution (same-file → same-pkg → global)
    trace.go           — Trace: BFS/DFS with depth limit, cycle detection, bidirectional
    repo.go            — TraceRepo: orchestrator (ingest → parse → graph → trace)
  clean/               — Smart code cleaning for LLM context
    clean.go           — CleanSource: strip comments, collapse blanks, truncate
  compare/             — Code comparison engine
    compare.go         — Compare: structural diff of two RepoSnapshot objects
  analyze/             — Analysis orchestration (MCP tool handlers)
    analyze.go         — AnalyzeRepo, SearchSymbols, BuildDepGraph
  github/              — GitHub API client
    github.go          — FetchRepoMeta, FetchREADME
  llm/                 — LLM client (CLIProxyAPI)
    llm.go             — Complete: OpenAI-compatible chat completion

deploy/
  go-code.env          — Environment template (no real secrets)
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze a GitHub repo or local path. Clones, parses ASTs, answers natural-language questions about architecture and implementation. |
| `file_parse` | Parse a single source file with tree-sitter. Returns symbol table (functions, types, methods) or raw AST. |
| `code_compare` | Compare two repositories structurally: architecture, API design, dependency strategies, code quality. |
| `dep_graph` | Build and visualize the dependency graph of a repository. Output as Mermaid, Graphviz DOT, or JSON. |
| `symbol_search` | Search for symbols (functions, types, consts) across a repo by name pattern or wildcard. |
| `call_trace` | Trace call chains from a function: callees (forward) or callers (reverse) with depth control and LLM narrative. |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_PORT` | `8897` | HTTP server port |
| `LLM_URL` | `http://127.0.0.1:8317/v1` | CLIProxyAPI base URL |
| `LLM_API_KEY` | (required) | API key for LLM proxy |
| `LLM_MODEL` | `gemini-2.5-flash` | Model name |
| `GITHUB_TOKEN` | (optional) | GitHub token for private repos and higher rate limits |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Directory for temporary clones |
| `MAX_FILE_KB` | `512` | Max file size to parse (KB) |
| `MAX_REPO_MB` | `200` | Max repo size to accept (MB) |

## Build & Deploy

```bash
# Local build (CGO required for tree-sitter)
make build       # → bin/go-code

# Lint
make lint        # golangci-lint run ./...

# Tests
make test        # go test ./...

# Docker deploy (via ~/deploy/example-server/docker-compose.yml)
make deploy      # docker compose build --no-cache + up -d
```

## Adding a New Tool

1. Create `cmd/go-code/tool_<name>.go` with input type and `register<Name>` function
2. Add `register<Name>(server, cfg)` call to `registerTools()` in `register.go`
3. Increment `toolCount` constant in `main.go`
4. Implement backing logic in the appropriate `internal/` package
5. Update tool count and description in this CLAUDE.md

## Adding a New Language

1. Create `internal/parser/queries/<lang>.scm` with tree-sitter query patterns
2. Create `internal/parser/handler_<lang>.go` implementing `LanguageHandler` interface
3. Register the handler in `init()` via `registerHandler("<lang>", ...)`
4. Add test sample `internal/parser/testdata/sample.<ext>`
5. Add `TestParse<Lang>File` in `internal/parser/parser_test.go`
6. Update this CLAUDE.md

The `LanguageHandler` interface requires: `Language()`, `Extensions()`, `Grammar()`, `QuerySource()`, `MapSymbol()`.

## CGO Requirement

tree-sitter grammars are C libraries. This means:
- Local builds need `CGO_ENABLED=1` and a C compiler (`gcc` or `clang`)
- Docker builder stage uses `golang:1.24-alpine` with `apk add gcc musl-dev`
- `.goreleaser.yaml` sets `CGO_ENABLED=1`
- Cross-compilation requires target C toolchains

## Conventions

- All internal packages are self-contained with no circular dependencies
- ingest → parser → clean → analyze → llm (dependency direction)
- compare, analyze, and callgraph are peers; none imports the others
- github package has no dependencies on other internal packages
- Tool handlers in `cmd/go-code/tool_*.go` import `internal/analyze` only
- Error messages use lowercase, wrap with `fmt.Errorf("context: %w", err)`
- Context is always the first parameter; never store context in structs
- HTTP clients always use context via `http.NewRequestWithContext`

## Deployment (Docker)

The service runs as a Docker container in `~/deploy/example-server/docker-compose.yml`.

```yaml
go-code:
  build:
    context: /path/to/repos/src/go-code
  ports:
    - "127.0.0.1:8897:8897"
  volumes:
    - /path/to/repos/src:/host-src:ro
  env_file:
    - .env
  restart: unless-stopped
```

**Note**: Local paths are mounted at `/host-src/` inside the container. When analyzing local repos via MCP, use the host path — the server translates automatically.

Register as MCP server after deployment:
```bash
claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp
```
