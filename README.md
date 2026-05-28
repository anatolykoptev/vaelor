# go-code

Code intelligence [MCP](https://modelcontextprotocol.io/) server powered by [tree-sitter](https://tree-sitter.github.io/tree-sitter/) AST parsing. Analyzes repositories, compares implementations, traces call chains, and searches symbols across any codebase — GitHub or local.

## Features

- **14 languages** — Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#, PHP, Svelte, Astro, Kotlin
- **30 MCP tools** — code search, AST analysis, knowledge graph queries, observability ⇄ code (`debug_investigate`), structural rewrite, code review, design search
- **Multiple analysis modes** — deep (clone + AST + LLM), quick (GitHub Code Search), issues/PRs
- **Call chain tracing** — bidirectional BFS with cycle detection and LLM narrative
- **Code comparison** — three-pass symbol matching (exact/fuzzy/semantic) with quality verdicts
- **Knowledge graph** — Apache AGE graph with NL-to-Cypher query generation
- **Caching** — LRU in-memory + optional Redis L2 (200x speedup on repeated queries)
- **Learnings** — prior review verdicts auto-surface in `understand`

## Tools

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze a repository. Deep mode (AST + LLM), quick mode (GitHub Code Search), or issue/PR search |
| `repo_search` | Discover GitHub repos via parallel SearXNG + GitHub API search with LLM-ranked results |
| `file_parse` | Parse a single file with tree-sitter. Returns symbol table or raw AST |
| `code_compare` | Compare two repositories structurally — architecture, API design, code quality |
| `dep_graph` | Build dependency graph. Output as Mermaid, Graphviz DOT, or JSON |
| `symbol_search` | Search symbols (functions, types, consts) by name pattern across a repo |
| `call_trace` | Trace call chains — callees (forward) or callers (reverse) with depth control |
| `code_graph` | Query a persistent code knowledge graph in Apache AGE via natural language |
| `debug_investigate` | 7-phase prod incident root cause: Prom spikes + Jaeger failed traces + symbol resolution + callgraph walks + LLM fusion + runtime binary drift → ranked file:function |
| `semantic_search` | Hybrid RRF: BM25F + pgvector + 1-hop AGE graph expansion. Find by concept, not keyword |
| `understand` | Type-aware symbol deep-dive. Aggregates call_trace + symbol_search + complexity + tested_by + dead_code_score |
| `impact_analysis` | Blast radius up to depth 10. Direct callers, transitive callers, hotspot reordering by churn |
| `prepare_change` | Pre-change risk: impact + dead_code combined |
| `dead_code` | Cross-encoder confidence [0..1] per symbol (CE reranker), not flat list |
| `dataflow_analyze` | IL/CFG taint tracking + dead stores + SQL/cmd injection sinks |
| `rewrite` | Structural AST search-replace with $WILDCARDS across 14 languages, dry-run + apply |
| `review_pr` | Differential blast radius between git refs; persists per-symbol learnings |
| `review_delta` | Differential blast radius between git refs; persists per-symbol learnings |
| `code_research` | BM25F + embeddings + DAG expansion for 10k+ file monorepos |
| `design_search` | Find DESIGN.md systems by UI description (multilingual-e5-large 1024-dim) |
| `resolve_frame` | Unminify a JS stack frame via source maps |
| `site_analyze` | Tech stack + SEO audit, BFS crawler |
| `site_crawl` | BFS crawler |
| `code_health` | Repo grade A-F: complexity, test coverage, dep freshness, OSV vulns |
| `explore` | Quick repo overview, sub-second, no LLM, no clone for remote repos |
| `fleet_versions` | Diff pinned container image references in indexed source (Dockerfile, docker-compose*.yml) against deployed runtime containers. Probes local docker socket by default; `ssh://[user@]host[:port]` reaches remote hosts via the system ssh client (requires `GOCODE_FLEET_SSH_ENABLE=true`). Catches the "config aligned, source looks right, behaviour wrong" bug class where prod runs a different binary version than the repo pins. |
| `remember_graph_insights` | Persist learnings surfaced in future understand calls |
| `wp_plugin_search` | Search WordPress.org plugin directory |


> **Optional:** `dead_code` and `code_health` tools integrate with [ox-codes](https://github.com/anatolykoptev/ox-codes), an internal Rust code analysis service. Without it, these tools degrade to AST-only heuristics and still produce useful results.

## Runtime version awareness

When a production bug lives at the **deployed binary** level rather than in
source — pinned tag drift, sibling-host divergence, silent auto-updates —
go-code can probe the running containers and diff them against what the
indexed repo pins.

Two surfaces:

1. `fleet_versions` — explicit tool. Pass `host` (optional, defaults to local
   docker socket) and `service` (optional filter). Returns per-target image
   diff with status: `Match` / `TagDrift` / `DigestDrift` / `OnlySource` /
   `OnlyRuntime` / `Unresolved`.

2. `debug_investigate` — Phase 7 runs automatically when an investigation is
   started with `host` set. Drift is included in the LLM prompt with
   priority-capped cardinality (top 20 non-Match diffs, sorted
   TagDrift > DigestDrift > Unresolved > OnlyRuntime > OnlySource).

### SSH probe

Reaching a remote host uses the system `ssh` binary directly — go-code does
not maintain its own SSH stack. This means `~/.ssh/config` (ProxyJump, agent,
identities, port, known_hosts) is the single source of truth and you get all
of it for free. The driver is disabled by default; enable with
`GOCODE_FLEET_SSH_ENABLE=true`.

Commands executed on the remote host are limited to an internal allowlist:
exactly `docker ps --no-trunc --format={{json .}}`. Filter values are
regex-validated before any exec call.

### Configuration

| Env | Default | Purpose |
|---|---|---|
| `GOCODE_FLEET_DEFAULT_HOST` | `""` | Fallback host for `debug_investigate` Phase 7. Empty = skip. |
| `GOCODE_FLEET_DOCKER_SOCKET` | `/var/run/docker.sock` | Local docker engine socket path. |
| `GOCODE_FLEET_SSH_ENABLE` | `false` | Security gate. Must be `true` to use `ssh://` targets. |
| `GOCODE_FLEET_SSH_BINARY` | `ssh` | System ssh binary; PATH-resolved by default. |
| `GOCODE_FLEET_TIMEOUT` | `10s` | Per-probe timeout. |

## Quick Start

### Docker (recommended)

```bash
docker build -t go-code .
docker run -p 8897:8897 \
  -e LLM_API_BASE=http://host.docker.internal:8317/v1 \
  -e LLM_API_KEY=your-key \
  go-code
```

### From source

Requires Go 1.24+ and a C compiler (CGO for tree-sitter grammars).

```bash
make build    # → bin/go-code
./bin/go-code
```

### Register as MCP server

```bash
claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp
```

## Usage Examples

### Analyze a GitHub repo
```json
{
  "tool": "repo_analyze",
  "arguments": {
    "repo": "golang/go",
    "query": "How does the garbage collector work?"
  }
}
```

### Quick code search (no cloning)
```json
{
  "tool": "repo_analyze",
  "arguments": {
    "repo": "anthropics/claude-code",
    "query": "MCP tool registration",
    "mode": "quick"
  }
}
```

### Search issues and PRs
```json
{
  "tool": "repo_analyze",
  "arguments": {
    "repo": "golang/go",
    "query": "generics performance",
    "type": "issue"
  }
}
```

### Compare two implementations
```json
{
  "tool": "code_compare",
  "arguments": {
    "repo_a": "gin-gonic/gin",
    "repo_b": "labstack/echo",
    "query": "middleware architecture and routing"
  }
}
```

### Trace call chains
```json
{
  "tool": "call_trace",
  "arguments": {
    "repo": "owner/repo",
    "function": "handleRequest",
    "direction": "callees",
    "depth": 5
  }
}
```

### Analyze a local directory
```json
{
  "tool": "repo_analyze",
  "arguments": {
    "repo": "/home/user/src/my-project",
    "query": "How is authentication implemented?"
  }
}
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_PORT` | `8897` | HTTP server port |
| `LLM_API_BASE` | `http://127.0.0.1:8317/v1` | OpenAI-compatible LLM endpoint |
| `LLM_API_KEY` | *(optional)* | API key for LLM — see LLM dependency below |
| `LLM_MODEL` | `gemini-2.5-flash` | Model name |
| `GITHUB_TOKEN` | *(optional)* | GitHub token for higher API rate limits |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Temp directory for cloned repos |
| `MAX_FILE_KB` | `512` | Max file size to parse (KB) |
| `MAX_REPO_MB` | `200` | Max repo size to accept (MB) |
| `REDIS_URL` | *(optional)* | Redis URL for L2 cache |
| `DATABASE_URL` | *(optional)* | PostgreSQL DSN for Apache AGE code graph |
| `SEARXNG_URL` | `http://searxng:8888` | SearXNG instance for `repo_search` |

### LLM dependency

`LLM_API_KEY` is optional. The server starts and most tools operate without it. The exact behavior per tool category when `LLM_API_KEY` is unset:

| Category | Tools | Behavior without `LLM_API_KEY` |
|----------|-------|-------------------------------|
| **Hard** | `code_graph` (NL query), `repo_search` | Returns MCP error: *"requires LLM_API_KEY to be set"* |
| **Soft** | `repo_analyze` (quick/raw modes), `repo_analyze_issues` | Returns deterministic results + `(LLM unavailable)` marker |
| **Augment** | `call_trace`, `dead_code`, `impact_analysis` | Returns full core output; narrative/augmentation fields are empty |
| **Debug** | `debug_investigate` | Runs deterministic phases (trace analysis, metric spikes, alert violations); LLM hypothesis ranking is skipped with `LLMSkippedReason: "LLM_API_KEY not set"` |

Set `LLM_API_BASE` + `LLM_API_KEY` + `LLM_MODEL` to enable all tools. Any OpenAI-compatible endpoint works (OpenAI, Anthropic via proxy, local Ollama, etc.).

## Architecture

```
cmd/go-code/          — MCP server, tool handlers (one file per tool)
internal/
  parser/             — tree-sitter AST parsing, 13 language handlers
  ingest/             — repo cloning, file walking, gitignore filtering
  clean/              — smart code cleaning for LLM context
  render/             — rendering modes (signatures, skeleton, focused)
  analyze/            — analysis orchestration
  compare/            — structural diff engine
  callgraph/          — call chain tracing (BFS/DFS, bidirectional)
  codegraph/          — Apache AGE knowledge graph
  github/             — GitHub API (search code/issues/repos, metadata)
  search/             — SearXNG web search client
  llm/                — LLM client with retry + fallback keys
  cache/              — generic LRU cache with Redis L2
  retry/              — exponential backoff with jitter
  metrics/            — atomic operation counters
```

## Analysis Modes

### Deep mode (default)
Clones the repo, walks the file tree, parses ASTs with tree-sitter, builds a symbol table, and answers questions via LLM. Supports `depth` (overview/module/deep) and `mode` (signatures/skeleton/focused) for controlling context size.

### Quick mode (`mode=quick`)
Uses GitHub Code Search API — no cloning. Returns code fragments matching the query, optionally summarized by LLM. Use `mode=raw` for fragments without LLM processing.

### Issues/PRs mode (`type=issue` or `type=pr`)
Searches GitHub Issues/Pull Requests API. Returns structured results with state, labels, author, and LLM analysis of trends and patterns.

## Transport

- **HTTP** (default): Streamable HTTP on `MCP_PORT`
- **Stdio**: `./go-code --stdio` — for pipe/SSH access

## Build

```bash
make build      # Build binary (CGO required)
make lint       # Run golangci-lint
make test       # Run tests
make deploy     # Docker build + deploy
```

## License

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE) Apache 2.0 — Copyright 2026 Anatoly Koptev

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to add new tools and languages.

For security vulnerabilities, see [SECURITY.md](SECURITY.md).
