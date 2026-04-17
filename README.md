# go-code

Code intelligence [MCP](https://modelcontextprotocol.io/) server powered by [tree-sitter](https://tree-sitter.github.io/tree-sitter/) AST parsing. Analyzes repositories, compares implementations, traces call chains, and searches symbols across any codebase — GitHub or local.

## Features

- **13 languages** — Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#, PHP, Svelte, Astro
- **8 MCP tools** — from quick code search to deep structural analysis
- **Multiple analysis modes** — deep (clone + AST + LLM), quick (GitHub Code Search), issues/PRs
- **Call chain tracing** — bidirectional BFS with cycle detection and LLM narrative
- **Code comparison** — three-pass symbol matching (exact/fuzzy/semantic) with quality verdicts
- **Knowledge graph** — Apache AGE graph with NL-to-Cypher query generation
- **Caching** — LRU in-memory + optional Redis L2 (200x speedup on repeated queries)

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
| `LLM_API_KEY` | *(required)* | API key for LLM |
| `LLM_MODEL` | `gemini-2.5-flash` | Model name |
| `GITHUB_TOKEN` | *(optional)* | GitHub token for higher API rate limits |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Temp directory for cloned repos |
| `MAX_FILE_KB` | `512` | Max file size to parse (KB) |
| `MAX_REPO_MB` | `200` | Max repo size to accept (MB) |
| `REDIS_URL` | *(optional)* | Redis URL for L2 cache |
| `DATABASE_URL` | *(optional)* | PostgreSQL DSN for Apache AGE code graph |
| `SEARXNG_URL` | `http://searxng:8888` | SearXNG instance for `repo_search` |

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

MIT
