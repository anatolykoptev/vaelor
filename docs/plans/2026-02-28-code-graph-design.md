# Phase 4.2: Code Graph — Design Document

**Date**: 2026-02-28
**Status**: Approved

## Overview

Add persistent code graph storage using Apache AGE (PostgreSQL extension) to go-code.
One new MCP tool `code_graph(repo, query)` enables natural-language queries against
a graph of packages, files, symbols, and their relationships.

## Architecture

```
cmd/go-code/
  tool_code_graph.go        — MCP tool registration + handler

internal/codegraph/
  schema.go                 — AGE graph schema (create_graph, vertex/edge labels)
  store.go                  — GraphStore: upsert vertices/edges, execute Cypher
  index.go                  — IndexRepo: ingest → parse → build graph in AGE
  query.go                  — QueryGraph: classify → execute → format
  templates.go              — ~10 Cypher query templates
  classify.go               — LLM classifier: NL → template + params, or "freeform"
  generate.go               — LLM freeform Cypher generator (fallback)
  templates_test.go
  classify_test.go
  generate_test.go
  store_test.go
  index_test.go
  query_test.go
```

### Data Flow

```
code_graph(repo, query, language?, refresh?)
  → resolveRoot(repo)
  → repo_key = fnv32(canonical_path)
  → check code_graph_meta (fresh? stale? missing?)
    → stale/missing: IndexRepo
      → ingest.IngestRepo → parser.ParseFile → callgraph.BuildCallGraph
      → batch upsert: Packages → Files → Symbols → CONTAINS → CALLS → IMPORTS
      → INSERT code_graph_meta
    → fresh: skip indexing
  → QueryGraph(query)
    → classify(query) → template match OR freeform
    → execute Cypher via ag_catalog.cypher()
    → LLM formats narrative
  → return JSON result
```

## Graph Schema (Apache AGE)

**Graph name**: `code_<fnv32(repo_path)>` — e.g. `code_a1b2c3d4`

### Vertex Labels

| Label | Properties | Source |
|-------|-----------|--------|
| `Package` | `name`, `path`, `repo` | Directory with source files |
| `File` | `path`, `language`, `lines` | `ingest.FileInfo` |
| `Symbol` | `name`, `kind`, `signature`, `file`, `start_line`, `end_line` | `parser.Symbol` |

`kind` values: function, method, type, struct, interface, class, const, var, module

### Edge Labels

| Label | From → To | Properties | Source |
|-------|-----------|-----------|--------|
| `CONTAINS` | Package → File, File → Symbol | — | Hierarchy |
| `CALLS` | Symbol → Symbol | `line` | `callgraph.CallEdge` |
| `IMPORTS` | File → Package | `alias` | `parser.ParseResult.Imports` |

### Relational Metadata Table

```sql
CREATE TABLE IF NOT EXISTS code_graph_meta (
    repo_key     TEXT PRIMARY KEY,
    repo_path    TEXT NOT NULL,
    graph_name   TEXT NOT NULL,
    file_count   INT,
    symbol_count INT,
    edge_count   INT,
    built_at     TIMESTAMPTZ NOT NULL,
    ttl_seconds  INT DEFAULT 3600
);
```

## NL→Cypher (Hybrid Approach)

### Step 1: Classification (LLM flash)

LLM receives template list + NL query → returns JSON:
- `{"template": "who_calls", "params": {"name": "ServeHTTP"}}` — template path
- `{"template": "freeform", "params": {}}` — fallback to generation

### Query Templates

| ID | Question | Cypher Pattern |
|----|----------|---------------|
| `who_calls` | Who calls X? | `MATCH (c:Symbol)-[:CALLS]->(t:Symbol {name: $name}) RETURN c` |
| `calls_of` | What does X call? | `MATCH (s:Symbol {name: $name})-[:CALLS]->(c:Symbol) RETURN c` |
| `imports_of` | What does file X import? | `MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS $path RETURN p` |
| `importers_of` | Who imports package X? | `MATCH (f:File)-[:IMPORTS]->(p:Package {name: $name}) RETURN f` |
| `symbols_in` | What's in file/package X? | `MATCH (c)-[:CONTAINS]->(s:Symbol) WHERE c.path CONTAINS $path RETURN s` |
| `call_chain` | Path from X to Y | `MATCH path = shortestPath((a:Symbol {name: $from})-[:CALLS*..10]->(b:Symbol {name: $to})) RETURN path` |
| `most_connected` | Most called functions | `MATCH (s:Symbol)<-[:CALLS]-(c) RETURN s, count(c) ORDER BY count DESC LIMIT $limit` |
| `dead_code` | Uncalled functions | `MATCH (s:Symbol) WHERE s.kind='function' AND NOT ()-[:CALLS]->(s) RETURN s` |
| `depends_on` | Package X dependencies | `MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS $pkg RETURN DISTINCT p` |
| `dependents_of` | Who depends on package X? | `MATCH (f:File)-[:IMPORTS]->(p:Package {name: $name}) RETURN DISTINCT f` |

### Step 2: Execution

- **Template path**: substitute params → `ag_catalog.cypher()` → result
- **Freeform path**: LLM generates Cypher from schema + query → read-only validation
  (reject `CREATE|DELETE|SET|MERGE|REMOVE|DROP`) → execute → on error, one retry with error message

### Step 3: Formatting

LLM (flash) receives raw Cypher result + original query → narrative answer + structured data.

### Output Format

```json
{
  "repo": "go-code",
  "query": "who calls ParseFile?",
  "template": "who_calls",
  "cypher": "MATCH ...",
  "results": [
    {"name": "AnalyzeRepo", "kind": "function", "file": "internal/analyze/analyze.go", "line": 42}
  ],
  "narrative": "ParseFile is called from 2 functions: ...",
  "graph_stats": {"vertices": 342, "edges": 1205, "cached": true}
}
```

## Lazy Indexing + TTL

### TTL Strategy

| Repo Type | Default TTL | Rationale |
|-----------|-------------|-----------|
| Local path | 1 hour | Code changes frequently |
| GitHub (remote) | 24 hours | Clone is expensive |

Optional `refresh: true` parameter forces re-indexing.

### Batch Upsert

UNWIND-based batches (~100 items per Cypher statement):
```cypher
UNWIND [{name:'foo', kind:'function', ...}, ...] AS s
MERGE (sym:Symbol {name: s.name, file: s.file})
SET sym.kind = s.kind, sym.signature = s.signature, ...
```

### Stale Graph Handling

Stale graph → `DROP GRAPH code_<hash> CASCADE` → DELETE meta row → rebuild.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| `DATABASE_URL` not set | `code_graph` tool not registered |
| AGE not installed | Error on first IndexRepo |
| LLM classifier returns garbage | Fallback to freeform |
| Freeform Cypher invalid | One retry with error context |
| Freeform contains write ops | Reject before execution (regex guard) |
| Cypher timeout (>30s) | `context.WithTimeout` → cancel |

## Configuration (env vars)

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | (optional) | PostgreSQL DSN. Without it `code_graph` is unavailable |
| `GRAPH_TTL_LOCAL` | `3600` | TTL seconds for local repos |
| `GRAPH_TTL_REMOTE` | `86400` | TTL seconds for GitHub repos |
| `GRAPH_BATCH_SIZE` | `100` | Batch size for UNWIND upsert |

## Testing

| What | How | File |
|------|-----|------|
| Cypher templates: syntax, param substitution | Unit test | `templates_test.go` |
| Classifier: NL→template mapping | Unit test: 20+ NL examples | `classify_test.go` |
| Read-only guard: reject write Cypher | Unit test | `generate_test.go` |
| Batch upsert: UNWIND generation | Unit test | `store_test.go` |
| IndexRepo: parse → graph build | Integration test (PG+AGE) | `index_test.go` |
| QueryGraph: end-to-end | Integration test | `query_test.go` |
| TTL: stale detection, refresh | Unit test with mock time | `index_test.go` |
| Graceful degradation: no DB | Unit test | `tool_code_graph_test.go` |

Integration tests tagged `//go:build integration` — not run in CI without PG+AGE.

## Decisions

- **One MCP tool** (`code_graph`) — keeps the interface simple
- **Shared PostgreSQL** from krolik-server — no new infrastructure
- **Package + File + Symbol granularity** — full hierarchy for all query types
- **Hybrid NL→Cypher** — templates for reliability, LLM freeform for flexibility
- **No IMPLEMENTS edges** for now — tree-sitter can't reliably extract interface implementations
- **Graceful degradation** — go-code works without DB, `code_graph` simply unavailable
