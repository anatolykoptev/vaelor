<!-- HERO DEMO: asciinema/GIF of understand or call_trace on golang/go — to record before launch -->

# go-code

**Give your coding agent a memory of the codebase it can't get from grep.**

go-code is the open-source engine behind [Krolik](https://krolik.tools): a self-hosted [MCP](https://modelcontextprotocol.io/) server that parses, graphs, and watches a codebase so an AI agent doesn't have to re-discover it every session. Tree-sitter AST parsing across 16 languages feeds a call graph with type-aware Go resolution (`go/types`), a persistent Apache AGE knowledge graph, and hybrid semantic search. `review_pr` remembers what broke last time a symbol was reviewed. `debug_investigate` correlates a Prometheus alert and a Jaeger trace back to the function that caused it.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)](go.mod)

[Quick Start](#quick-start) in three commands. If it's useful, a star helps the next person find it.

## Why not just grep, or a code index?

Every code-search tool finds the function. Fewer tell an agent what else breaks when it changes, and fewer still remember your last review or your last production incident.

| | Org-wide code search | Session-scoped agent tools | Hosted PR-review bots | go-code |
|---|---|---|---|---|
| What it's for | full-text and symbol search across an indexed org, surfaced to agents as context | navigation and editing inside one agent session | automated review comments on a PR, as a hosted service | search, call graph, blast radius, and live incidents behind one MCP endpoint you run yourself |

go-code's Apache AGE graph carries PageRank, community, and surprise scores computed from your actual call graph, and those scores feed `understand`, `prepare_change`, and `review_pr` directly. `review_pr` also persists a verdict per symbol from the real GitHub review outcome, and `understand` surfaces it automatically the next time anyone touches that symbol, so a "this broke in review" fact travels with the code instead of living in someone's memory. `debug_investigate` goes further: it fuses a live Prometheus alert and a Jaeger trace with the same call graph into a ranked `file:function` hypothesis, so an incident resolves to code, not just a dashboard alert. An agent's sense of which change is risky comes from the shape of your actual codebase, recomputed on every graph update.

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

### Register as an MCP server

```bash
claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp
```

### Try it

No local clone needed for the first call: `understand` and `call_trace` accept a remote GitHub repo directly and fetch it on demand.

```json
{
  "tool": "call_trace",
  "arguments": {
    "repo": "golang/go",
    "symbol": "ListenAndServe",
    "direction": "callees",
    "depth": 3
  }
}
```

Returns the callee chain from `net/http.ListenAndServe` with cycle detection, no Docker volume mount and no `DATABASE_URL` required.

No `LLM_API_KEY` yet? Most tools still run: they skip only the narrative and ranking layer. See [LLM dependency](#llm-dependency) for exactly what degrades and what doesn't.

## What it does

### Understand a codebase
`explore`: fast, no-clone overview for remote repos. `understand`: symbol deep-dive covering callers, callees, complexity, dead-code score, and any prior review verdicts on that symbol. `code_research`: BM25F plus embeddings plus graph expansion for 10k+ file monorepos.

### Know what a change will break
`impact_analysis`: configurable blast-radius depth (default 5), hotspots reordered by churn. `prepare_change`: impact analysis and dead-code check in one call. `call_trace`: bidirectional call chains with cycle detection and an LLM narrative.

### Search by meaning, not keyword
`semantic_search`: pgvector plus hybrid RRF plus 1-hop graph expansion. `symbol_search`, `code_search`, `github_code_search` (no clone required).

### Review and remember
`review_pr` / `review_delta`: differential blast radius between git refs, posting to GitHub and persisting a per-symbol learning on approval or rejection. `code_compare`: structural diff between two repos covering architecture, API design, quality. `rewrite`: structural AST search-replace across 16 languages, dry-run first.

### Debug what's actually running, not just what's in the diff
`debug_investigate`: 7-phase incident root cause, from metric spikes and failed traces through symbol resolution, callgraph walks, LLM fusion, and runtime binary drift, ranked to a `file:function`. `fleet_versions`: diffs the image tags pinned in source against what's actually deployed. `dataflow`: taint tracking, dead stores, SQL/command-injection sinks. `dead_code` and `code_health`: an A-F grade covering complexity, test coverage, dependency freshness, OSV vulnerabilities.

### Also worth knowing about
`find_duplicates`: finds near-duplicate symbols in a repo via semantic similarity (needs `EMBED_URL` + `DATABASE_URL`). `suggest_reviewers`: ranks reviewer candidates for a PR's file paths from authorship, co-change coupling, and recency.

That's the headline set, not the whole one. The table below documents what's stable; call `tools/list` on a running server for the complete, current one.

## Tool Reference

The stable, documented set. The server exposes a few more; `tools/list` on a running instance is the authoritative, current list.

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze a repository. Deep mode (AST + LLM), quick mode (GitHub Code Search), or issue/PR search |
| `repo_search` | Discover repos across forges via parallel web search + GitHub/GitLab API, LLM-ranked |
| `github_code_search` | Search code on GitHub via the Code Search API. Returns file paths and matching fragments |
| `file_parse` | Parse a single file with tree-sitter. Returns symbol table or raw AST |
| `code_compare` | Compare two repositories structurally: architecture, API design, code quality |
| `dep_graph` | Build a dependency graph. Output as Mermaid, Graphviz DOT, or JSON |
| `symbol_search` | Search symbols (functions, types, consts) by name pattern across a repo |
| `code_search` | Grep-like search with regex and path-glob filtering, context lines included |
| `call_trace` | Trace call chains: callees (forward) or callers (reverse), with depth control |
| `code_graph` | Query the persistent code knowledge graph in Apache AGE via natural language |
| `debug_investigate` | 7-phase prod incident root cause: Prom spikes + Jaeger failed traces + symbol resolution + callgraph walks + LLM fusion + runtime binary drift, ranked to `file:function` |
| `semantic_search` | Hybrid RRF: BM25F + pgvector + 1-hop AGE graph expansion. Find by concept, not keyword |
| `understand` | Type-aware symbol deep-dive. Aggregates call_trace + symbol_search + complexity + tested_by + dead_code_score + prior learnings |
| `impact_analysis` | Configurable blast-radius depth (default 5). Direct callers, transitive callers, hotspot reordering by churn |
| `prepare_change` | Pre-change risk: impact analysis + dead-code check combined |
| `dead_code` | Confidence-scored unused-symbol detection, not a flat list |
| `dataflow` | IL/CFG taint tracking, dead stores, SQL/command-injection sinks |
| `rewrite` | Structural AST search-replace with `$WILDCARDS` across 16 languages, dry-run and apply |
| `review_pr` | Differential blast radius between git refs; persists per-symbol learnings |
| `review_delta` | Differential blast radius between two git refs |
| `code_research` | BM25F + embeddings + DAG expansion for 10k+ file monorepos |
| `design_search` | Find `DESIGN.md` systems by UI description (multilingual-e5-large, 1024-dim) |
| `resolve_frame` | Unminify a JS stack frame via source maps |
| `site_analyze` | Tech-stack and SEO audit, BFS crawler |
| `site_crawl` | BFS web crawler |
| `code_health` | Repo grade A-F: complexity, test coverage, dependency freshness, OSV vulnerabilities |
| `explore` | Fast repo overview, no LLM, no clone for remote repos |
| `fleet_versions` | Diff pinned container-image references in source against deployed runtime containers. Catches the "config aligned, source looks right, behavior wrong" bug class |
| `find_duplicates` | Find near-duplicate symbols in one repo via semantic similarity. Requires `EMBED_URL` + `DATABASE_URL` |
| `suggest_reviewers` | Rank reviewer candidates for a PR's file paths from authorship, co-change coupling, and recency |
| `remember_graph_insights` | Persist a learning so it surfaces in future `understand` calls |
| `wp_plugin_search` | Search the WordPress.org plugin directory |

> **Optional sidecars.** `dead_code` and `code_health` sharpen from AST-only heuristics to confidence-scored results when [ox-codes](https://github.com/anatolykoptev/ox-codes) (Rust) is running alongside. `semantic_search` needs an embedding endpoint; [ox-embed-server](https://github.com/anatolykoptev/ox-embed-server) (ONNX embeddings, cross-encoder rerank, SPLADE) is the reference one. Neither is required to run go-code, and every tool without them still returns a real result, just a coarser one.

## Learnings loop

`review_pr` (with `dry_run=false`) writes one learning per changed symbol on a completed GitHub review: `APPROVE` maps to `good`, `REQUEST_CHANGES` maps to `bad`, everything else is `neutral`. The dry-run path writes a `risk_level` on the same record. The next time anyone calls `understand` on that symbol, `Store.Nearest` looks up the closest prior learnings and attaches them as `prior_learnings` on the result, no extra call needed. The loop is gated on `LEARNINGS_DATABASE_URL` (falls back to `DATABASE_URL`); with neither set, it silently no-ops instead of erroring.

## Graph signal ecosystem

Two graph representations cooperate through `internal/graphx` without either package knowing about the other, so every consumer below degrades to byte-identical output when there's no graph snapshot yet.

| Signal (AGE-computed) | Consumed by |
|---|---|
| PageRank, community | `understand` (`graph_analytics`), `prepare_change` (`communities_crossed`, `high_pagerank_callers`) |
| Surprise score | `review_pr` (`high_surprise` flag at ≥0.5) |
| Handles / fetches (cross-language routes) | `call_trace` (fetch nodes at +1 depth across service boundaries) |
| Tested-by | `impact_analysis` (`tests_covering`) |

## Runtime version awareness

When a production bug lives at the deployed-binary level rather than in source (pinned tag drift, sibling-host divergence, a silent auto-update), go-code can probe running containers and diff them against what the indexed repo pins.

- `fleet_versions`: the explicit tool. Pass `host` (defaults to the local Docker socket) and an optional `service` filter. Returns a per-target diff: `Match` / `TagDrift` / `DigestDrift` / `OnlySource` / `OnlyRuntime` / `Unresolved`.
- `debug_investigate` Phase 7: runs automatically when an investigation starts with `host` set. Drift enters the LLM prompt priority-capped at the top 20 non-`Match` diffs, sorted `TagDrift` > `DigestDrift` > `Unresolved` > `OnlyRuntime` > `OnlySource`.

### SSH probe

Reaching a remote host uses the system `ssh` binary directly; go-code doesn't maintain its own SSH stack, so `~/.ssh/config` (ProxyJump, agent, identities, port, known_hosts) is the single source of truth. The driver is off by default: enable with `GOCODE_FLEET_SSH_ENABLE=true`. Commands on the remote host are limited to an internal allowlist, exactly `docker ps --no-trunc --format={{json .}}`, with filter values regex-validated before any exec call.

| Env | Default | Purpose |
|---|---|---|
| `GOCODE_FLEET_DEFAULT_HOST` | `""` | Fallback host for `debug_investigate` Phase 7. Empty = skip |
| `GOCODE_FLEET_DOCKER_SOCKET` | `/var/run/docker.sock` | Local Docker engine socket path |
| `GOCODE_FLEET_SSH_ENABLE` | `false` | Must be `true` to use `ssh://` targets |
| `GOCODE_FLEET_SSH_BINARY` | `ssh` | System ssh binary, PATH-resolved by default |
| `GOCODE_FLEET_TIMEOUT` | `10s` | Per-probe timeout |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_PORT` | `8897` | HTTP server port |
| `LLM_API_BASE` | `http://127.0.0.1:8317/v1` | OpenAI-compatible LLM endpoint |
| `LLM_API_KEY` | *(optional)* | See [LLM dependency](#llm-dependency) below |
| `LLM_MODEL` | `gemini-3.1-flash-lite-preview` | Model name |
| `GITHUB_TOKEN` | *(optional)* | Higher API rate limits, private repos |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Temp directory for cloned repos |
| `MAX_FILE_KB` | `512` | Max file size to parse (KB) |
| `MAX_REPO_MB` | `200` | Max repo size to accept (MB) |
| `REDIS_URL` | *(optional)* | Redis URL for the L2 parse/LLM cache |
| `DATABASE_URL` | *(optional)* | PostgreSQL DSN for the Apache AGE code graph (required for `code_graph`, the learnings loop, and the graph signals above) |
| `EMBED_URL` | *(optional)* | Embedding server endpoint (required for `semantic_search`) |
| `GITHUB_WEBHOOK_SECRET` | *(optional)* | Set to enable the `/webhook/github` PR-review receiver |

Full environment reference (SearXNG, GitLab, fallback-model chains, fleet SSH, indexing tuning): [CLAUDE.md](CLAUDE.md).

### LLM dependency

`LLM_API_KEY` is optional. The server starts and most tools operate without it:

| Category | Tools | Behavior without `LLM_API_KEY` |
|----------|-------|-------------------------------|
| **Hard** | `code_graph` (NL query), `repo_search` | Returns an MCP error: *"requires LLM_API_KEY to be set"* |
| **Soft** | `repo_analyze` (quick/raw modes), issue/PR search | Returns deterministic results plus an `(LLM unavailable)` marker |
| **Augment** | `call_trace`, `dead_code`, `impact_analysis` | Full core output; narrative/augmentation fields are empty |
| **Debug** | `debug_investigate` | Runs the deterministic phases (trace analysis, metric spikes, alert violations); LLM hypothesis ranking is skipped with `LLMSkippedReason` set |

Set `LLM_API_BASE` + `LLM_API_KEY` + `LLM_MODEL` to run every tool at full capability. Any OpenAI-compatible endpoint works: OpenAI, Anthropic via proxy, a local Ollama instance.

## Architecture

```
cmd/go-code/     : MCP server, tool handlers (one file per tool)
internal/
  parser/        : tree-sitter AST parsing, 16 language handlers
  ingest/        : repo cloning, file walking, gitignore filtering
  clean/         : code cleaning for LLM context
  render/        : rendering modes (signatures, skeleton, focused)
  analyze/       : analysis orchestration
  compare/       : structural diff engine
  callgraph/     : call chain tracing (BFS/DFS, bidirectional), type-aware for Go
  codegraph/     : Apache AGE knowledge graph
  embeddings/    : semantic search: pgvector store, embed pipeline, hybrid RRF, graph expansion
  learnings/     : pgvector-backed store for prior review findings
  investigate/   : Prometheus + Jaeger correlation for debug_investigate
  fleet/         : deployed-vs-source container image drift
  github/        : GitHub API (search code/issues/repos, metadata)
  llm/           : LLM client with retry and fallback keys
  cache/         : generic LRU cache with Redis L2
```

## Analysis Modes

**Deep mode (default).** Clones the repo, walks the file tree, parses ASTs with tree-sitter, builds a symbol table, and answers questions via LLM. `depth` controls context size (overview/module/deep), `mode` controls rendering (signatures/skeleton/focused).

**Quick mode (`mode=quick`).** GitHub Code Search API, no cloning. Returns matching code fragments, optionally LLM-summarized. `mode=raw` skips the LLM step entirely.

**Issues/PRs mode (`type=issue` or `type=pr`).** Searches GitHub Issues/Pull Requests. Structured results with state, labels, author, and an LLM read on trends.

## Webhook

`POST /webhook/github` on the MCP port (`:8897`) handles `pull_request` events (opened/synchronize/reopened). Requires `X-GitHub-Event` and `X-Hub-Signature-256` headers and `GITHUB_WEBHOOK_SECRET` set. `REVIEW_POST_ENABLED=true` posts the review to GitHub; otherwise it dry-logs. Expose it through your existing tunnel and register it in the repo's webhook settings.

## Transport

- **HTTP** (default): streamable HTTP on `MCP_PORT`
- **Stdio**: `./go-code --stdio` for pipe/SSH access

## Build

```bash
make build      # Build binary (CGO required)
make lint       # Run golangci-lint
make test       # Run tests
make deploy     # Docker build + deploy
```

## License

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE) Apache 2.0, Copyright 2026 Anatoly Koptev.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for adding a new tool or a new language.

For security vulnerabilities, see [SECURITY.md](SECURITY.md). Please don't open a public issue for those.

---

If go-code saves your agent from re-discovering the same fact about your codebase twice, a star helps the next person find it before they build a fifth MCP code-search server from scratch.
