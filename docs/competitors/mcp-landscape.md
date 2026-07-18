# MCP Code Intelligence Servers — Landscape

Updated 2026-02-28. See individual files for deep dives on major competitors.

## Tier 1 (1000+ stars)

| Project | Stars | Lang | Approach | Key Differentiator | Deep Dive |
|---------|-------|------|----------|-------------------|-----------|
| [Serena](https://github.com/oraios/serena) | 20.8K | Python | LSP-backed, 40+ languages | True semantic understanding | [serena.md](serena.md) |
| [kit](https://github.com/cased/kit) | 1.3K | Python | tree-sitter + Chroma | AST pattern search, PR review | — |
| [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext) | 903 | Python | tree-sitter → FalkorDB/Neo4j | Dual graph backend, file watching | — |

## Tier 2 (100-1000 stars)

| Project | Stars | Lang | Approach | Key Differentiator | Deep Dive |
|---------|-------|------|----------|-------------------|-----------|
| [Axon](https://github.com/harshkedia177/axon) | 384 | Python | 12-phase → KuzuDB | Blast radius, dead code | [axon.md](axon.md) |
| [mcp-server-tree-sitter](https://github.com/wrale/mcp-server-tree-sitter) | 253 | Python | Cursor-based traversal | Parse caching, state persistence | — |
| [Octocode](https://github.com/Muvon/octocode) | 236 | Rust | GraphRAG + LSP | Rust performance, AI memory | — |
| [Axon.MCP.Server](https://github.com/ali-kamali/Axon.MCP.Server) | 151 | Python | 10-service micro | Multi-parser (tree-sitter + Roslyn) | — |

## Tier 3 (< 100 stars, architecturally interesting)

| Project | Stars | Lang | Key Feature | Deep Dive |
|---------|-------|------|-------------|-----------|
| [code-graph-mcp](https://github.com/entrepeneur4lyf/code-graph-mcp) | 80 | Python | ast-grep, 25+ langs, LRU cache | — |
| [CodeMCP](https://github.com/SimplyLiz/CodeMCP) | 62 | **Go** | 76 tools, SCIP, fusion ranking | [codemcp.md](codemcp.md) |
| [ast-mcp-server](https://github.com/angrysky56/ast-mcp-server) | 30 | Python | AST diff, ASG, Neo4j | — |
| [tree-sitter-mcp](https://github.com/nendotools/tree-sitter-mcp) | 27 | TypeScript | Minimal: 4 tools | — |
| [codeprism](https://github.com/rustic-ai/codeprism) | new | Rust | Universal AST, `RoutesTo` edges | — |

## MCP Server Patterns (Go)

### mark3labs/mcp-go
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) | 8,239 stars | Go
- De facto Go MCP SDK. Used by GitHub's official MCP server.
- Pattern: `NewTool` + functional options + `AddTool` + `ServeHTTP`.

### github/github-mcp-server
- [github/github-mcp-server](https://github.com/github/github-mcp-server) | 27,304 stars | Go
- Production Go MCP server. Middleware pattern, toolset grouping, context propagation.

## Registries

- [Official MCP servers](https://github.com/modelcontextprotocol/servers) — no dedicated code analysis server
- [awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers) — 7260+ servers, weak code analysis coverage

## Comparative Feature Matrix

| Feature | Vaelor | Serena | kit | Axon | CodeMCP |
|---------|---------|--------|-----|------|---------|
| **Language** | Go | Python | Python | Python | Go |
| **Parsing** | tree-sitter | LSP | tree-sitter | tree-sitter | SCIP + tree-sitter |
| **Languages** | 9 | 40+ | 15+ | multi | multi |
| **Call graph** | Yes (BFS) | No | No | Yes | Yes (SCIP) |
| **Graph DB** | Apache AGE | No | No | KuzuDB | In-memory |
| **NL→Cypher** | Yes | No | No | Yes | No |
| **Code compare** | Yes | No | No | No | No |
| **Impact/blast** | No | No | No | Yes | Yes |
| **Dead code** | No | No | No | Yes | Yes |
| **Semantic search** | No | No | Yes (Chroma) | Yes | No |
| **SCIP backend** | No | No | No | No | Yes |
| **Repo search** | Yes | No | Yes | No | No |
| **Cross-language** | Yes | No | No | No | No |
