# go-code — Code Intelligence MCP Server

**Module**: `github.com/anatolykoptev/go-code` | **Port**: 8897 | **MCP**: `http://127.0.0.1:8897/mcp`
**Languages**: Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#

## Package Overview

| Package | Role |
|---------|------|
| `cmd/go-code/` | MCP entry point; `tool_*.go` = one file per tool, `register.go` wires them |
| `internal/ingest/` | Repo clone + walk; `internal/parser/` = tree-sitter AST → symbols |
| `internal/analyze/` | Orchestration imported by tool handlers (tools import ONLY this) |
| `internal/codegraph/` | Apache AGE persistent graph; `internal/callgraph/` = in-memory call tracing |
| `internal/polyglot/` | Multi-language repo structure detection |
| `internal/routes/` | HTTP route extraction (7 languages, used for cross-language edges) |
| `internal/llm/` | CLIProxyAPI client with retry + fallback |

## MCP Tools

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze GitHub repo or local path. Modes: `deep` (AST+LLM), `quick` (GitHub Code Search), `pr`/`issue` |
| `file_parse` | Parse single file with tree-sitter → symbol table or raw AST |
| `code_compare` | Structural diff of two repos: architecture, API design, dependencies |
| `dep_graph` | Dependency graph as Mermaid/DOT/JSON. `cross_language=true` adds Route edges |
| `symbol_search` | Find functions/types/consts by name pattern or wildcard across a repo |
| `call_trace` | BFS/DFS call chain from a function; forward (callees) or reverse (callers) with LLM narrative |
| `code_graph` | Query persistent Apache AGE graph (`gocode` DB). 14 Cypher templates + LLM freeform fallback. Lazy indexing with TTL. Requires `DATABASE_URL` |
| `repo_search` | Discover GitHub repos. Parallel SearXNG + GitHub API, LLM-ranked |

## Environment Variables

| Variable | Default | Notes |
|----------|---------|-------|
| `MCP_PORT` | `8897` | |
| `LLM_API_BASE` | `http://127.0.0.1:8317/v1` | CLIProxyAPI |
| `LLM_API_KEY` | required | |
| `LLM_API_KEY_FALLBACK` | optional | Comma-separated, used on 429/5xx |
| `LLM_MODEL` | `gemini-2.5-flash` | |
| `GITHUB_TOKEN` | optional | Higher rate limits + private repos |
| `GITHUB_SEARCH_REPOS` | optional | Default repos for quick-mode code search |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Temp clone location |
| `MAX_FILE_KB` | `512` | |
| `MAX_REPO_MB` | `200` | |
| `SEARXNG_URL` | `http://searxng:8888` | |
| `REDIS_URL` | optional | L2 cache, DB 6 |
| `DATABASE_URL` | optional | PostgreSQL DSN for Apache AGE (`gocode` database) |
| `GRAPH_TTL_LOCAL` | `3600` | Seconds |
| `GRAPH_TTL_REMOTE` | `86400` | Seconds |
| `GRAPH_BATCH_SIZE` | `5` | Keep small — AGE limitation |

## Build

```bash
make build   # CGO_ENABLED=1 required (tree-sitter grammars are C)
make lint
make test
make deploy  # docker compose build --no-cache + up -d
```

## Conventions

- Dependency direction: `ingest → parser → clean → analyze → llm`
- `compare`, `analyze`, `callgraph` are peers — none imports the others
- `github` package has no dependencies on other internal packages
- Tool handlers (`cmd/go-code/tool_*.go`) import `internal/analyze` only — no direct internal package access
- Error messages: lowercase, `fmt.Errorf("context: %w", err)`
- Context always first param; never stored in structs
- HTTP clients always use `http.NewRequestWithContext`

## Gotchas

- **Apache AGE**: no `ON CREATE SET` / `ON MATCH SET` — use separate `CREATE` then `SET` statements
- **AGE batch size**: `GRAPH_BATCH_SIZE=5` — larger batches cause parse errors in AGE Cypher
- **code_graph DB name**: always `gocode` (not the service name, not configurable at runtime)
- **Local repo paths**: Docker mounts `/path/to/repos/src:/host-src:ro`; server translates host paths automatically — pass host path to MCP tools
- **MCP registration**: `claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp`

## Contributing

See `docs/contributing.md` for: adding a new tool, adding a new language, CGO details.
