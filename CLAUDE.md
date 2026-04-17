# go-code — Code Intelligence MCP Server

**Module**: `github.com/anatolykoptev/go-code` | **Port**: 8897 | **MCP**: `http://127.0.0.1:8897/mcp`
**Languages**: Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#, PHP

## Package Overview

| Package | Role |
|---------|------|
| `cmd/go-code/` | MCP entry point; `tool_*.go` = one file per tool, `register.go` wires them |
| `internal/ingest/` | Repo clone (`--filter=blob:none` partial clone) + walk |
| `internal/parser/` | tree-sitter AST → symbols (11 languages) |
| `internal/analyze/` | Orchestration imported by tool handlers (tools import ONLY this) |
| `internal/callgraph/` | In-memory call tracing + go/types type-aware resolution for Go repos |
| `internal/goanalysis/` | Go package loading (`go/packages`) + type-aware call resolution (`go/types`) |
| `internal/tier/` | 3-tier analysis system (Basic/Enhanced/Full) with degradation warnings |
| `internal/compound/` | Compound tools (`understand`, `prepare_change`) aggregating sub-queries |
| `internal/codegraph/` | Apache AGE persistent graph |
| `internal/embeddings/` | Semantic search: pgvector store, embed pipeline, hybrid RRF, graph expansion |
| `internal/codesearch/` | Grep-like code search with path filter support |
| `internal/freshness/` | Dependency freshness + CVE/vulnerability checking via OSV.dev API |
| `internal/polyglot/` | Multi-language repo structure detection |
| `internal/routes/` | HTTP route extraction (7 languages, used for cross-language edges) |
| `internal/forge/` | Multi-forge abstraction: `Forge` interface, GitHub + GitLab implementations, URL detection, registry |
| `internal/websearch/` | HTTP client for go-search MCP (smart_search depth=fast), used by repo_search |
| `internal/llm/` | CLIProxyAPI client with retry + fallback |
| `internal/clean/` | Symbol post-processing between parser and analyze |
| `internal/cache/` | Parse/LLM/tool caches (in-mem L1 + optional Redis L2) |
| `internal/compare/` | Repo structural comparison (backs `code_compare`) |
| `internal/deadcode/` | Unused-symbol detection (backs `dead_code`) |
| `internal/explore/` | Quick repo overview (backs `explore`) |
| `internal/impact/` | Change-impact analysis (backs `impact_analysis`) |
| `internal/research/` | Deep repository research (backs `code_research`) |
| `internal/review/` | PR / delta review (backs `review_*`) |
| `internal/designmd/` | Design-doc indexing (backs `design_search`) |
| `internal/oxcodes/` | ox-codes cross-service integration |
| `internal/webanalyze/` | Site technology fingerprinting |

## MCP Tools

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze GitHub/GitLab repo or local path. Modes: `deep` (AST+LLM), `quick` (Code Search), `pr`/`issue` |
| `file_parse` | Parse single file with tree-sitter → symbol table or raw AST |
| `code_compare` | Structural diff of two repos: architecture, API design, dependencies |
| `dep_graph` | Dependency graph as Mermaid/DOT/JSON. `cross_language=true` adds Route edges |
| `symbol_search` | Find functions/types/consts by name pattern or wildcard across a repo |
| `call_trace` | BFS/DFS call chain from a function; forward (callees) or reverse (callers) with LLM narrative |
| `code_graph` | Query persistent Apache AGE graph (`gocode` DB). 14 Cypher templates + LLM freeform fallback. Lazy indexing with TTL. Requires `DATABASE_URL` |
| `repo_search` | Discover repos across forges. Parallel web search (go-search) + GitHub/GitLab API, LLM-ranked |
| `code_search` | Grep-like search with regex, path filter (`file_glob`), context lines |
| `dead_code` | Find unused exported functions/types |
| `explore` | Quick overview: stats, README, deps, health |
| `code_health` | Quality grade (A-F), 14 sub-scores incl. CVE/vulnerability checking via OSV.dev |
| `impact_analysis` | Blast radius of changing a function |
| `semantic_search` | Vector similarity search via pgvector + hybrid RRF + graph expansion |
| `understand` | Deep-dive symbol analysis. Aggregates: symbol info + callees + callers + complexity. Type-aware for Go |
| `prepare_change` | Pre-change risk assessment. Aggregates: impact_analysis + dead_code check |
| `site_analyze` | Web site tech analysis |
| `site_crawl` | BFS web crawler |
| `review_pr` | Review a pull request: differential impact analysis on all changes |
| `review_delta` | Analyze changes between two git refs and compute differential impact |
| `review_pr_post` | Run review_pr and post the result as a PR review on GitHub |
| `code_research` | Deep repo research — AST traversal + LLM narrative + test linkage |
| `rewrite` | Automated refactor suggestions / code rewrite |
| `dataflow` | Taint / dead-value data-flow analysis (sub-modes: dead, taint) |
| `design_search` | Semantic search across design docs (requires `DESIGN_EMBED_URL`) |
| `wp_plugin_search` | Search the WordPress.org plugin directory |

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
| `REDIS_URL` | optional | L2 cache, DB 6 |
| `DATABASE_URL` | optional | PostgreSQL DSN for Apache AGE (`gocode` database) |
| `GRAPH_TTL_LOCAL` | `3600` | Seconds |
| `GRAPH_TTL_REMOTE` | `86400` | Seconds |
| `GRAPH_BATCH_SIZE` | `5` | Keep small — AGE limitation |
| `GITLAB_TOKEN` | optional | GitLab API token (`PRIVATE-TOKEN` header) |
| `GITLAB_URL` | optional | Self-hosted GitLab base URL (default: `https://gitlab.com`) |
| `GO_SEARCH_URL` | optional | go-search MCP endpoint for web search (e.g. `http://go-search:8890/mcp`) |
| `EMBED_URL` | optional | Embedding server (e.g. `http://embed-jina:8083`) — enables semantic_search |
| `EMBED_MODEL` | `jina-code-v2` | Model name for OpenAI-compatible embed API |
| `AUTO_INDEX_DIRS` | optional | Comma-separated dirs to auto-index for semantic search (e.g. `/host/src`) |
| `PATH_MAPPINGS` | optional | Host-to-container path mapping (e.g. `/home/krolik:/host`) |
| `OUTPUT_DIR` | optional | Output dir for generated files (e.g. `/tmp/go-code-output`) |
| `GITHUB_WEBHOOK_SECRET` | optional | When set, enables `/webhook/github` PR-review receiver |
| `REVIEW_POST_ENABLED` | `false` | When `true`, webhook posts reviews; otherwise dry-log |
| `REVIEW_BOT_USER` | optional | GitHub login to skip (avoid self-triggered loops) |

## Webhook

### GitHub webhook
- Endpoint: `POST /webhook/github` on MCP port (:8897)
- Headers required: `X-GitHub-Event`, `X-Hub-Signature-256`
- Events handled: `pull_request` (opened/synchronize/reopened)
- Setup: expose via your existing tunnel (e.g. dozor/Cloudflare) and register in GitHub repo settings

## Build

```bash
# All go commands need GOWORK=off — the user-wide go.work interferes.
make build   # CGO_ENABLED=1 required (tree-sitter grammars are C)
make lint
make test
make deploy  # docker compose build --no-cache + up -d
```

## Conventions

- Dependency direction: `ingest → parser → clean → analyze → llm`
- `compare`, `analyze`, `callgraph` are peers — none imports the others (except `callgraph` imports `goanalysis` for type-aware resolution)
- `forge` package has no dependencies on other internal packages
- Tool handlers (`cmd/go-code/tool_*.go`) import `internal/analyze` only — no direct internal package access
- Error messages: lowercase, `fmt.Errorf("context: %w", err)`
- Context always first param; never stored in structs
- HTTP clients always use `http.NewRequestWithContext`

## Gotchas

- **Apache AGE**: no `ON CREATE SET` / `ON MATCH SET` — use separate `CREATE` then `SET` statements
- **AGE batch size**: `GRAPH_BATCH_SIZE=5` — larger batches cause parse errors in AGE Cypher
- **code_graph DB name**: always `gocode` (not the service name, not configurable at runtime)
- **Local repo paths**: Docker mounts `/home/krolik:/host:ro`; `PATH_MAPPINGS=/home/krolik:/host` translates paths automatically
- **Partial clone**: `--filter=blob:none` reduces memory for large repos (no blob download during clone)
- **Semantic search stack**: embed-jina (jina-code-v2, 768 dim) → pgvector (HNSW cosine) → hybrid RRF (semantic + keyword) → graph expansion (1-hop AGE)
- **MCP registration**: `claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp`
- **`GOWORK=off` mandatory**: without it, `go test`/`go build` resolve against the user-wide `go.work` and break imports
- **Extension → language is single source of truth via `handler.Extensions()`** — don't duplicate in a separate map (historical bug: `.cjs` was in handler but missing from the lang map, making `ParseFile("foo.cjs")` fail silently)

## Contributing

See `docs/contributing.md` for: adding a new tool, adding a new language, CGO details.
