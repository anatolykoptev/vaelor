# ox-codes: Rust Code Search Backend for go-code

## Problem

go-code's `internal/codesearch` package (240 lines Go) is sequential, loads entire files into memory, and has no literal search fast path. For 10K+ file repos, search takes ~2.5s. No AST-aware or structural search capabilities.

## Solution

New Rust HTTP service `ox-codes` (:8902) using ripgrep crates as the search engine. go-code calls it via HTTP, replacing `codesearch.Search()`.

## Architecture

```
Claude/User → go-code (MCP :8897) → ox-codes (HTTP :8902)
                                         ↓
                                   ripgrep crates (grep-regex, grep-searcher, ignore)
                                   + tree-sitter (AST scoping)
                                   + ast-grep-core (structural search)
```

ox-codes is an **internal backend** — no MCP, no public API. Only go-code talks to it.

## Rust Crate Stack

| Crate | Version | Role |
|-------|---------|------|
| `grep-regex` | 0.1 | Regex engine (implements `Matcher` trait) |
| `grep-searcher` | 0.1 | Line-oriented search: mmap, binary detection, context lines |
| `ignore` | 0.4 | Parallel file walker with gitignore support |
| `globset` | 0.4 | Multi-pattern glob matching (NFA-compiled) |
| `tree-sitter` | 0.24 | AST parsing for scoped search |
| `ast-grep-core` | 0.38 | Structural pattern matching with $WILDCARDS |
| `axum` | 0.8 | HTTP framework |
| `tokio` | 1.x | Async runtime |
| `serde`/`serde_json` | 1.x | JSON serialization |

## Project Layout

```
~/src/ox-codes/
├── Cargo.toml              (workspace)
├── crates/
│   ├── core/               (search engine)
│   │   ├── src/
│   │   │   ├── lib.rs      (public API)
│   │   │   ├── grep.rs     (ripgrep-based text search)
│   │   │   ├── scoped.rs   (tree-sitter scoped search)
│   │   │   ├── structural.rs (ast-grep structural search)
│   │   │   ├── walker.rs   (file walking + filtering)
│   │   │   └── types.rs    (shared types)
│   │   └── Cargo.toml
│   ├── server/             (HTTP endpoints)
│   │   ├── src/
│   │   │   ├── main.rs     (entrypoint)
│   │   │   ├── routes.rs   (axum handlers)
│   │   │   └── state.rs    (app state)
│   │   └── Cargo.toml
│   └── langs/              (language configs)
│       ├── src/
│       │   ├── lib.rs      (language registry)
│       │   ├── go.rs       (Go scopes)
│       │   ├── rust.rs     (Rust scopes)
│       │   ├── python.rs   (Python scopes)
│       │   ├── typescript.rs
│       │   └── java.rs
│       └── Cargo.toml
├── Dockerfile
├── Makefile
└── CLAUDE.md
```

## API Endpoints

### POST /search — grep replacement

Request:
```json
{
  "root": "/path/to/repo",
  "pattern": "HandleRequest",
  "is_regex": false,
  "file_glob": "*.go",
  "exclude_glob": "vendor/*,*_test.go",
  "context_lines": 2,
  "max_results": 50,
  "case_sensitive": true
}
```

Response:
```json
{
  "matches": [
    {
      "file": "internal/server/handler.go",
      "line": 42,
      "text": "func HandleRequest(ctx context.Context) error {",
      "context": ["// HandleRequest processes...", "func HandleRequest..."]
    }
  ],
  "total_matches": 156,
  "truncated": true,
  "duration_ms": 45
}
```

Features from ripgrep crates (free):
- mmap for large files
- Binary file detection and skip
- Encoding transcoding (UTF-16, etc.)
- SIMD-accelerated regex
- Parallel file walking (crossbeam work-stealing)
- Gitignore respect
- Literal search fast path (no regex overhead)

### POST /search/scoped — AST-aware search

Request:
```json
{
  "root": "/path/to/repo",
  "pattern": "TODO|FIXME",
  "scope": "function_bodies",
  "language": "go",
  "max_results": 50
}
```

Scopes: `function_bodies`, `comments`, `strings`, `type_definitions`, `imports`

Implementation: tree-sitter parses file → extracts scope ranges → grep-searcher searches only within those byte ranges.

### POST /search/structural — pattern matching

Request:
```json
{
  "root": "/path/to/repo",
  "pattern": "if $ERR != nil { return $ERR }",
  "language": "go",
  "max_results": 50
}
```

Implementation: ast-grep-core parses pattern + source → returns structural matches with captures.

### GET /health

Returns `{"status": "ok"}`.

## go-code Integration

### New package: `internal/oxcodes/`

```go
// client.go — HTTP client to ox-codes
type Client struct { baseURL string; http *http.Client }
func (c *Client) Search(ctx, SearchInput) ([]SearchMatch, error)
func (c *Client) SearchScoped(ctx, ScopedSearchInput) ([]SearchMatch, error)
func (c *Client) SearchStructural(ctx, StructuralInput) ([]SearchMatch, error)
```

### Modified: `cmd/go-code/tool_code_search.go`

- If `OX_CODES_URL` is set → call `oxcodes.Client.Search()`
- Else → fallback to existing `codesearch.Search()` (graceful degradation)
- New MCP tool parameters: `scope` (optional), `structural` (bool)

### Environment

```env
OX_CODES_URL=http://ox-codes:8902  # in docker-compose
```

## Docker

New service in `~/deploy/krolik-server/docker-compose.yml`:

```yaml
ox-codes:
  build:
    context: ../../src/ox-codes
    dockerfile: Dockerfile
  container_name: ox-codes
  ports:
    - "8902:8902"
  volumes:
    - /home/krolik/src:/host-src:ro  # same as go-code
  environment:
    - PORT=8902
    - RUST_LOG=info
  restart: unless-stopped
```

## Performance Expectations

| Metric | Go codesearch | ox-codes |
|--------|--------------|----------|
| 10K files, 500 matches | ~2.5s | ~400-800ms |
| Literal search | Via regex | Direct string match (SIMD) |
| File walking | Sequential | Parallel (crossbeam) |
| Memory per file | Full file in []string | mmap or streaming |
| Binary detection | Via ingest | Built-in (grep-searcher) |

## Non-Goals

- ox-codes does NOT expose MCP (go-code is the MCP layer)
- ox-codes does NOT clone repos (go-code handles cloning, passes local paths)
- ox-codes does NOT do LLM summarization (go-code does that)
- No persistent index — each search is stateless (rely on OS page cache)
