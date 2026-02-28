# Phase 4.3: Cross-Language Analysis — Design

## Goal

Add cross-language awareness to go-code: detect polyglot repo structure, extract HTTP routes and API boundaries, link backend handlers to frontend fetch calls, and expose everything through the existing MCP tools and Apache AGE graph.

## Architecture

Graph-First approach: extend the AGE schema with new vertex types (Layer, Route) and edge types (BELONGS_TO, HANDLES, FETCHES). All cross-language data is queryable via Cypher through the existing `code_graph` tool.

Three new internal packages:
- `internal/polyglot/` — repo structure detection
- `internal/routes/` — HTTP route extraction (patterns + LLM fallback)
- Graph extensions in `internal/codegraph/` — new vertices, edges, templates

No new MCP tools. Existing tools gain cross-language capabilities.

## Tech Stack

- Apache AGE (PostgreSQL) for graph storage
- tree-sitter for source parsing (existing)
- Regex patterns for route extraction from known frameworks
- LLM (Gemini via CLIProxyAPI) for fallback route detection and narrative

---

## Component 1: Polyglot Detection (`internal/polyglot/`)

### Types

```go
type RepoStructure struct {
    Layers    []Layer
    Languages map[string]int   // language → file count
    Manifests []Manifest
}

type Layer struct {
    Name     string   // "backend", "frontend", "ml-pipeline", "shared"
    Role     string   // "server", "client", "worker", "library"
    RootDir  string   // "backend/", "frontend/src/"
    Language string   // dominant language in this layer
    Files    int
}

type Manifest struct {
    Path     string   // "backend/go.mod"
    Type     string   // "go.mod", "package.json", "Cargo.toml", etc.
    Language string
}
```

### Detection Strategy

1. **Manifest scan**: Find `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `requirements.txt`, `pom.xml`, `build.gradle`, `*.csproj`, `Gemfile` in the repo tree.
2. **Directory grouping**: Group files by their nearest manifest root. Count languages per group.
3. **Role classification** (heuristic):
   - Has `http.ListenAndServe`, `express()`, `Flask(__name__)`, `SpringApplication.run` → `server`
   - Has `fetch()`, `XMLHttpRequest`, `ReactDOM.render`, `document.querySelector` → `client`
   - Has `torch`, `tensorflow`, `sklearn`, `pandas` → `worker` (ML)
   - Otherwise → `library`
4. **LLM fallback**: If manifests are absent or structure is ambiguous, send file tree to LLM with prompt: "Classify this repo's directory structure into layers."

### Graph Schema

- Vertex: `(:Layer {name, role, language, root_dir})`
- Edge: `(:File)-[:BELONGS_TO]->(:Layer)`

---

## Component 2: Route Extraction (`internal/routes/`)

### Types

```go
type Route struct {
    Method    string   // "GET", "POST", "*"
    Path      string   // "/api/users/:id"
    Handler   string   // symbol name
    Framework string   // "chi", "express", "flask", etc.
    File      string
    Line      uint32
    Side      string   // "server" or "client"
}

type RouteMatcher interface {
    Language() string
    Frameworks() []string
    Match(source []byte, symbols []*parser.Symbol, calls []*parser.CallSite) []Route
}
```

### Built-in Matchers

**Server-side** (route definitions):

| Language | Frameworks | Example Patterns |
|----------|-----------|-----------------|
| Go | net/http, chi, gin, echo, gorilla/mux | `r.HandleFunc("/path", handler)`, `r.Get("/path", handler)`, `e.GET("/path", handler)` |
| TypeScript | Express, Fastify, Hono, Nest | `app.get("/path", handler)`, `@Get("/path")` |
| Python | Flask, FastAPI, Django | `@app.route("/path")`, `@router.get("/path")`, `path("api/", views)` |
| Java | Spring Boot | `@GetMapping("/path")`, `@RequestMapping(value="/path")` |
| Rust | Actix-web, Axum, Rocket | `.route("/path", web::get())`, `#[get("/path")]` |
| Ruby | Rails, Sinatra | `get '/path' => 'controller#action'`, `resources :users` |
| C# | ASP.NET | `[HttpGet("/path")]`, `app.MapGet("/path", handler)` |

**Client-side** (HTTP calls / fetches):

| Language | Patterns |
|----------|---------|
| TypeScript | `fetch("/path")`, `axios.get("/path")`, `$.ajax({url: "/path"})` |
| Python | `requests.get("/path")`, `httpx.get("/path")`, `aiohttp.get("/path")` |
| Go | `http.Get("/path")`, `http.NewRequestWithContext(ctx, "GET", "/path", ...)` |
| Java | `HttpClient`, `RestTemplate.getForObject("/path")` |
| Ruby | `Net::HTTP.get("/path")`, `HTTParty.get("/path")` |
| Rust | `reqwest::get("/path")`, `Client::new().get("/path")` |
| C# | `HttpClient.GetAsync("/path")` |

**LLM fallback**: If a file imports HTTP-related packages but no patterns match, send the file's symbols + first 200 lines to LLM: "Extract HTTP endpoints (method, path, handler name) from this code."

### Matching Algorithm

1. For each file, check language → get registered matchers
2. Run regex patterns against source code
3. For each match, resolve handler name to a Symbol from the parse result
4. Classify as server or client based on pattern type
5. If no matches but file has HTTP imports → LLM fallback

### Graph Schema

- Vertex: `(:Route {method, path, framework})`
- Edge: `(:Symbol)-[:HANDLES]->(:Route)` (server-side handler)
- Edge: `(:Symbol)-[:FETCHES]->(:Route)` (client-side caller)

### Cross-Language Linking

Routes are linked by path: if a Go handler `HANDLES` route `GET /api/users` and a TypeScript function `FETCHES` route `GET /api/users`, they share the same Route vertex in the graph. This creates an implicit cross-language connection queryable via Cypher.

Path matching uses normalization:
- Strip path parameters: `/users/:id` → `/users/*`
- Case-insensitive comparison
- Optional prefix stripping (e.g., `/api/v1/` → `/v1/`)

---

## Component 3: Graph Extensions (`internal/codegraph/`)

### IndexRepo Changes

After existing vertex/edge insertion, add two new phases:
1. **Polyglot phase**: `DetectStructure()` → insert Layer vertices + BELONGS_TO edges
2. **Routes phase**: `ExtractRoutes()` → insert Route vertices + HANDLES/FETCHES edges

### New Cypher Templates (4)

```
api_routes:
  "Find HTTP routes, optionally filtered by path"
  Params: [path]
  MATCH (s:Symbol)-[r:HANDLES|FETCHES]->(route:Route)
  WHERE route.path CONTAINS '{path}'
  RETURN s.name, s.file, type(r), route.method, route.path

cross_calls:
  "Find symbols from different languages connected through a route"
  Params: [path]
  MATCH (server:Symbol)-[:HANDLES]->(route:Route)<-[:FETCHES]-(client:Symbol)
  WHERE route.path CONTAINS '{path}'
  RETURN server.name, server.file, route.method, route.path, client.name, client.file

layer_deps:
  "Show dependencies between architectural layers"
  Params: []
  MATCH (f1:File)-[:BELONGS_TO]->(l1:Layer),
        (f2:File)-[:BELONGS_TO]->(l2:Layer),
        (s1:Symbol)<-[:CONTAINS]-(f1),
        (s1)-[:CALLS]->(s2),
        (s2)<-[:CONTAINS]-(f2)
  WHERE l1 <> l2
  RETURN l1.name, l2.name, count(*) AS connections
  ORDER BY connections DESC

polyglot_overview:
  "Show repository structure: layers, languages, route counts"
  Params: []
  MATCH (l:Layer)<-[:BELONGS_TO]-(f:File)
  OPTIONAL MATCH (f)-[:CONTAINS]->(s:Symbol)-[:HANDLES]->(r:Route)
  RETURN l.name, l.role, l.language, count(DISTINCT f) AS files, count(DISTINCT r) AS routes
```

### Tool Extensions

**`dep_graph`**: New parameter `cross_language: bool` (default false).
When true: include cross-language Route edges in the dependency output (Mermaid/DOT/JSON).

**`repo_analyze`**: When `depth: deep` and repo is polyglot (2+ languages with 5+ files each):
- Add "Cross-Language Architecture" section to LLM system prompt
- Include layer structure, API boundaries, shared routes in the context

**`code_graph`**: The 4 new templates are automatically available. NL classifier updated to recognize cross-language queries.

---

## Testing Strategy

- Unit tests for each matcher (known framework patterns)
- Unit tests for polyglot detection (mock file trees)
- Unit tests for route normalization and path matching
- Integration test: index a real polyglot repo, query cross-language routes
- Smoke test via MCP: `code_graph` with cross-language queries

---

## Scope Boundaries

**In scope**:
- HTTP REST API boundaries (routes, handlers, fetch calls)
- Polyglot structure detection (layers, manifests, language grouping)
- Unified view via Cypher graph queries
- 7 languages for server-side, 7 languages for client-side (skip C/C++)

**Out of scope**:
- gRPC/protobuf boundary detection
- WebSocket endpoint mapping
- Shared type validation (TypeScript interfaces matching Go structs)
- Runtime analysis or dynamic tracing
