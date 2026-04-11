# ox-codes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Rust HTTP service `ox-codes` (:8902) that replaces go-code's Go codesearch with ripgrep-powered grep + tree-sitter scoped search + ast-grep structural search.

**Architecture:** Rust Cargo workspace with 3 crates (core, langs, server). go-code calls ox-codes via HTTP, falls back to Go codesearch if unavailable.

**Tech Stack:** Rust 1.93, axum 0.8.8, grep-regex 0.1.14/grep-searcher 0.1.16/ignore 0.4.25/globset 0.4.6 (ripgrep), tree-sitter 0.26.7, ast-grep-core 0.42.0, tokio 1.50, serde_json.

**Spec:** `docs/superpowers/specs/2026-03-23-ox-codes-design.md`

---

## File Structure

```
~/src/ox-codes/
├── Cargo.toml                    # Workspace root + binary package
├── src/
│   ├── main.rs                   # CLI + server entrypoint (clap)
│   └── serve.rs                  # axum router setup
├── crates/
│   ├── core/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       ├── lib.rs            # pub use re-exports
│   │       ├── types.rs          # SearchInput, SearchMatch, ScopedInput, StructuralInput
│   │       ├── grep.rs           # ripgrep-based text search (grep-searcher + ignore)
│   │       ├── scoped.rs         # tree-sitter scoped search (regex within AST regions)
│   │       └── structural.rs    # ast-grep structural pattern matching
│   ├── langs/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       ├── lib.rs            # ScopeKind enum, language registry
│   │       ├── go.rs             # Go tree-sitter queries
│   │       ├── rust.rs           # Rust tree-sitter queries
│   │       ├── python.rs         # Python tree-sitter queries
│   │       ├── typescript.rs     # TypeScript tree-sitter queries
│   │       └── java.rs           # Java tree-sitter queries
│   └── server/
│       ├── Cargo.toml
│       └── src/
│           ├── lib.rs            # pub router(), AppState
│           ├── search.rs         # POST /search handler
│           ├── scoped.rs         # POST /search/scoped handler
│           └── structural.rs    # POST /search/structural handler
├── Dockerfile
├── Makefile
└── CLAUDE.md
```

**go-code changes:**
```
~/src/go-code/
├── internal/oxcodes/
│   └── client.go                 # HTTP client to ox-codes
└── cmd/go-code/
    └── tool_code_search.go       # Modified: oxcodes fallback
```

**Deploy:**
```
~/deploy/krolik-server/
└── docker-compose.yml            # New ox-codes service
```

---

## Task 1: Scaffold Rust workspace

**Files:**
- Create: `~/src/ox-codes/Cargo.toml`
- Create: `~/src/ox-codes/crates/core/Cargo.toml`
- Create: `~/src/ox-codes/crates/core/src/lib.rs`
- Create: `~/src/ox-codes/crates/core/src/types.rs`
- Create: `~/src/ox-codes/crates/langs/Cargo.toml`
- Create: `~/src/ox-codes/crates/langs/src/lib.rs`
- Create: `~/src/ox-codes/crates/server/Cargo.toml`
- Create: `~/src/ox-codes/crates/server/src/lib.rs`
- Create: `~/src/ox-codes/src/main.rs`
- Create: `~/src/ox-codes/src/serve.rs`
- Create: `~/src/ox-codes/Makefile`
- Create: `~/src/ox-codes/CLAUDE.md`

- [ ] **Step 1: Create workspace Cargo.toml**

```toml
[workspace]
resolver = "2"
members = ["crates/core", "crates/langs", "crates/server"]

[workspace.package]
version = "0.1.0"
edition = "2024"
license = "MIT"
authors = ["Anatoly Koptev"]

[workspace.dependencies]
tokio = { version = "1", features = ["full"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
anyhow = "1"
thiserror = "2"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }

[package]
name = "ox-codes"
version.workspace = true
edition.workspace = true

[dependencies]
ox-core = { path = "crates/core" }
ox-langs = { path = "crates/langs" }
ox-server = { path = "crates/server" }
clap = { version = "4", features = ["derive", "env"] }
tokio.workspace = true
tracing.workspace = true
tracing-subscriber.workspace = true
anyhow.workspace = true
```

- [ ] **Step 2: Create crates/core/Cargo.toml**

```toml
[package]
name = "ox-core"
version.workspace = true
edition.workspace = true

[dependencies]
ox-langs = { path = "../langs" }
grep-regex = "0.1"
grep-searcher = "0.1"
grep-matcher = "0.1"
ignore = "0.4"
globset = "0.4"
tree-sitter = "0.26"
ast-grep-core = "0.42"
serde.workspace = true
serde_json.workspace = true
anyhow.workspace = true
thiserror.workspace = true
tracing.workspace = true

[dev-dependencies]
tempfile = "3"
```

- [ ] **Step 3: Create crates/langs/Cargo.toml**

```toml
[package]
name = "ox-langs"
version.workspace = true
edition.workspace = true

[dependencies]
tree-sitter = "0.24"
tree-sitter-go = "0.23"
tree-sitter-rust = "0.24"
tree-sitter-python = "0.23"
tree-sitter-javascript = "0.23"
tree-sitter-typescript = "0.23"
tree-sitter-java = "0.23"
# Note: verify exact versions with `cargo add` — tree-sitter grammar crates
# must be compatible with tree-sitter 0.26
```

- [ ] **Step 4: Create crates/server/Cargo.toml**

```toml
[package]
name = "ox-server"
version.workspace = true
edition.workspace = true

[dependencies]
ox-core = { path = "../core" }
axum = "0.8"
tokio.workspace = true
serde.workspace = true
serde_json.workspace = true
tracing.workspace = true
anyhow.workspace = true
```

- [ ] **Step 5: Create stub source files**

`crates/core/src/types.rs`:
```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize)]
pub struct SearchInput {
    pub root: String,
    pub pattern: String,
    #[serde(default)]
    pub is_regex: bool,
    #[serde(default)]
    pub file_glob: Option<String>,
    #[serde(default)]
    pub exclude_glob: Option<String>,
    #[serde(default = "default_context_lines")]
    pub context_lines: usize,
    #[serde(default = "default_max_results")]
    pub max_results: usize,
    #[serde(default = "default_true")]
    pub case_sensitive: bool,
    #[serde(default)]
    pub language: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ScopedSearchInput {
    pub root: String,
    pub pattern: String,
    pub scope: String,
    pub language: String,
    #[serde(default)]
    pub is_regex: bool,
    #[serde(default = "default_max_results")]
    pub max_results: usize,
    #[serde(default = "default_true")]
    pub case_sensitive: bool,
}

#[derive(Debug, Deserialize)]
pub struct StructuralSearchInput {
    pub root: String,
    pub pattern: String,
    pub language: String,
    #[serde(default = "default_max_results")]
    pub max_results: usize,
}

#[derive(Debug, Serialize)]
pub struct SearchResponse {
    pub matches: Vec<SearchMatch>,
    pub total_matches: usize,
    pub truncated: bool,
    pub duration_ms: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct SearchMatch {
    pub file: String,
    pub line: usize,
    pub text: String,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub context: Vec<String>,
}

fn default_context_lines() -> usize { 2 }
fn default_max_results() -> usize { 50 }
fn default_true() -> bool { true }
```

`crates/core/src/lib.rs`:
```rust
pub mod grep;
pub mod scoped;
pub mod structural;
pub mod types;

pub use types::*;
```

`crates/langs/src/lib.rs`:
```rust
pub enum ScopeKind {
    FunctionBodies,
    Comments,
    Strings,
    TypeDefinitions,
    Imports,
}
```

`crates/server/src/lib.rs`:
```rust
pub mod search;
pub mod scoped;
pub mod structural;

use axum::{Router, routing::{get, post}};

pub fn router() -> Router {
    Router::new()
        .route("/health", get(|| async { "ok" }))
        .route("/search", post(search::handle))
        .route("/search/scoped", post(scoped::handle))
        .route("/search/structural", post(structural::handle))
}
```

`src/main.rs`:
```rust
mod serve;

use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "ox-codes", version, about = "Rust code search backend")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    Serve {
        #[arg(long, env = "PORT", default_value = "8902")]
        port: u16,
    },
    Version,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();
    let cli = Cli::parse();
    match cli.command {
        Commands::Serve { port } => serve::run(port).await,
        Commands::Version => {
            println!("ox-codes {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
    }
}
```

`src/serve.rs`:
```rust
use std::net::SocketAddr;
use tracing::info;

pub async fn run(port: u16) -> anyhow::Result<()> {
    let app = ox_server::router();
    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    info!("ox-codes listening on {addr}");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
```

- [ ] **Step 6: Create Makefile**

```makefile
.PHONY: build test lint fmt check deploy

build:
	cargo build --workspace

test:
	cargo test --workspace

lint:
	cargo clippy --workspace -- -D warnings

fmt:
	cargo fmt --all

check: fmt lint test
	@echo "All checks passed"

deploy:
	cd ~/deploy/krolik-server && docker compose build --no-cache ox-codes && docker compose up -d --no-deps --force-recreate ox-codes
```

- [ ] **Step 7: Create CLAUDE.md**

Minimal CLAUDE.md with build/deploy/port/crate info.

- [ ] **Step 8: Initialize git repo, verify build**

```bash
cd ~/src/ox-codes && git init && cargo build --workspace
```

- [ ] **Step 9: Commit scaffold**

```bash
git add -A && git commit -m "feat: scaffold ox-codes workspace"
```

---

## Task 2: Implement grep search (crates/core/src/grep.rs)

**Files:**
- Create: `~/src/ox-codes/crates/core/src/grep.rs`
- Test: inline `#[cfg(test)]` + integration test with tempdir

This is the core replacement for Go's `codesearch.Search()`. Uses `grep-searcher` + `grep-regex` + `ignore` + `globset`.

- [ ] **Step 1: Write failing test**

```rust
// In crates/core/src/grep.rs
#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;
    use std::fs;

    fn setup_repo() -> TempDir {
        let dir = TempDir::new().unwrap();
        fs::create_dir_all(dir.path().join("src")).unwrap();
        fs::write(dir.path().join("src/main.go"), "package main\n\nfunc HandleRequest() {\n\t// TODO: implement\n}\n").unwrap();
        fs::write(dir.path().join("src/util.go"), "package main\n\nfunc helper() {}\n").unwrap();
        fs::write(dir.path().join("vendor/dep.go"), "package dep\n").unwrap();
        dir
    }

    #[test]
    fn test_literal_search() {
        let dir = setup_repo();
        let input = SearchInput {
            root: dir.path().to_string_lossy().into(),
            pattern: "HandleRequest".into(),
            is_regex: false,
            case_sensitive: true,
            max_results: 50,
            context_lines: 0,
            ..Default::default()
        };
        let result = search(input).unwrap();
        assert_eq!(result.matches.len(), 1);
        assert_eq!(result.matches[0].line, 3);
        assert!(result.matches[0].text.contains("HandleRequest"));
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cargo test -p ox-core test_literal_search -- --nocapture
```

- [ ] **Step 3: Implement grep search**

Core implementation using ripgrep crates:
- `ignore::WalkBuilder` for parallel file traversal with gitignore
- `grep_regex::RegexMatcher` for pattern compilation (literal or regex)
- `grep_searcher::Searcher` with custom `Sink` for match collection
- `globset::GlobSet` for file_glob/exclude_glob filtering
- Context lines via `SearcherBuilder::before_context/after_context`
- Match density ranking (port from Go)

Key functions:
```rust
pub fn search(input: SearchInput) -> anyhow::Result<SearchResponse>
fn build_matcher(pattern: &str, is_regex: bool, case_sensitive: bool) -> Result<RegexMatcher>
fn build_glob_filter(file_glob: Option<&str>, exclude_glob: Option<&str>) -> Result<GlobFilter>
fn collect_files(root: &Path, filter: &GlobFilter, language: Option<&str>) -> Vec<PathBuf>
fn search_file(path: &Path, root: &Path, matcher: &RegexMatcher, ctx_lines: usize) -> Vec<SearchMatch>
fn rank_by_density(matches: &mut Vec<SearchMatch>)
```

- [ ] **Step 4: Run test, verify pass**

```bash
cargo test -p ox-core -- --nocapture
```

- [ ] **Step 5: Add more tests**

Tests for: regex search, case insensitive, file glob, exclude glob, context lines, max results truncation, empty results, binary file skip.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: implement ripgrep-based grep search"
```

---

## Task 3: Implement language scopes (crates/langs/)

**Files:**
- Modify: `~/src/ox-codes/crates/langs/src/lib.rs`
- Create: `~/src/ox-codes/crates/langs/src/go.rs`
- Create: `~/src/ox-codes/crates/langs/src/rust.rs`
- Create: `~/src/ox-codes/crates/langs/src/python.rs`
- Create: `~/src/ox-codes/crates/langs/src/typescript.rs`
- Create: `~/src/ox-codes/crates/langs/src/java.rs`

Each language file provides tree-sitter S-expression queries for each `ScopeKind`.

- [ ] **Step 1: Write test for Go scopes**

```rust
#[test]
fn test_go_function_bodies_query() {
    let lang = get_language("go").unwrap();
    let query_str = get_scope_query("go", ScopeKind::FunctionBodies).unwrap();
    // Must compile without error
    tree_sitter::Query::new(&lang, &query_str).unwrap();
}
```

- [ ] **Step 2: Implement language registry**

`lib.rs`: `ScopeKind` enum, `get_language(name) -> Language`, `get_scope_query(lang, scope) -> &str`, `detect_language(path) -> Option<&str>`.

- [ ] **Step 3: Implement Go scopes**

Tree-sitter queries for Go:
- `FunctionBodies`: `(function_declaration body: (block) @scope)` + `(method_declaration body: (block) @scope)`
- `Comments`: `(comment) @scope`
- `Strings`: `(interpreted_string_literal) @scope` + `(raw_string_literal) @scope`
- `TypeDefinitions`: `(type_declaration) @scope`
- `Imports`: `(import_declaration) @scope`

- [ ] **Step 4: Implement Rust, Python, TypeScript, Java scopes**

Same pattern for each language with language-specific node types.

- [ ] **Step 5: Run tests**

```bash
cargo test -p ox-langs -- --nocapture
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add tree-sitter language scopes for 5 languages"
```

---

## Task 4: Implement scoped search (crates/core/src/scoped.rs)

**Files:**
- Create: `~/src/ox-codes/crates/core/src/scoped.rs`

Two-pass algorithm: tree-sitter extracts scope byte ranges → grep-searcher searches only within those ranges.

- [ ] **Step 1: Write failing test**

```rust
#[test]
fn test_scoped_search_function_bodies() {
    let dir = setup_repo();
    let input = ScopedSearchInput {
        root: dir.path().to_string_lossy().into(),
        pattern: "TODO".into(),
        scope: "function_bodies".into(),
        language: "go".into(),
        ..Default::default()
    };
    let result = scoped_search(input).unwrap();
    assert_eq!(result.matches.len(), 1); // TODO is inside function body
}

#[test]
fn test_scoped_search_comments_only() {
    let dir = setup_repo();
    let input = ScopedSearchInput {
        root: dir.path().to_string_lossy().into(),
        pattern: "TODO".into(),
        scope: "comments".into(),
        language: "go".into(),
        ..Default::default()
    };
    let result = scoped_search(input).unwrap();
    assert_eq!(result.matches.len(), 1); // TODO is in a comment
}
```

- [ ] **Step 2: Implement scoped search**

Algorithm:
1. Walk files (same as grep, filtered by language)
2. For each file: parse with tree-sitter → run scope query → get byte ranges
3. For each scope range: extract text → run regex match → collect matches with line numbers

Key functions:
```rust
pub fn scoped_search(input: ScopedSearchInput) -> anyhow::Result<SearchResponse>
fn extract_scope_ranges(source: &[u8], lang: &str, scope: ScopeKind) -> Vec<Range<usize>>
fn search_within_ranges(source: &[u8], ranges: &[Range<usize>], matcher: &RegexMatcher, rel_path: &str) -> Vec<SearchMatch>
```

- [ ] **Step 3: Run tests, verify pass**

```bash
cargo test -p ox-core test_scoped -- --nocapture
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: implement tree-sitter scoped search"
```

---

## Task 5: Implement structural search (crates/core/src/structural.rs)

**Files:**
- Create: `~/src/ox-codes/crates/core/src/structural.rs`

Uses `ast-grep-core` for `$WILDCARD` pattern matching.

- [ ] **Step 1: Write failing test**

```rust
#[test]
fn test_structural_search_go_error_pattern() {
    let dir = TempDir::new().unwrap();
    fs::write(dir.path().join("main.go"),
        "package main\n\nfunc foo() error {\n\tif err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
    ).unwrap();
    let input = StructuralSearchInput {
        root: dir.path().to_string_lossy().into(),
        pattern: "if $ERR != nil { return $ERR }".into(),
        language: "go".into(),
        max_results: 50,
    };
    let result = structural_search(input).unwrap();
    assert_eq!(result.matches.len(), 1);
}
```

- [ ] **Step 2: Implement structural search**

Algorithm:
1. Walk files filtered by language
2. For each file: parse with `ast-grep-core` using the pattern
3. Collect matches with captures, file paths, line numbers

Key functions:
```rust
pub fn structural_search(input: StructuralSearchInput) -> anyhow::Result<SearchResponse>
```

- [ ] **Step 3: Run tests, verify pass**

```bash
cargo test -p ox-core test_structural -- --nocapture
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: implement ast-grep structural search"
```

---

## Task 6: Implement HTTP server (crates/server/)

**Files:**
- Modify: `~/src/ox-codes/crates/server/src/lib.rs`
- Create: `~/src/ox-codes/crates/server/src/search.rs`
- Create: `~/src/ox-codes/crates/server/src/scoped.rs`
- Create: `~/src/ox-codes/crates/server/src/structural.rs`

- [ ] **Step 1: Implement POST /search handler**

```rust
// crates/server/src/search.rs
use axum::Json;
use ox_core::{SearchInput, SearchResponse, grep};

pub async fn handle(Json(input): Json<SearchInput>) -> Json<SearchResponse> {
    match grep::search(input) {
        Ok(resp) => Json(resp),
        Err(e) => Json(SearchResponse::error(e.to_string())),
    }
}
```

Note: search is CPU-bound → use `tokio::task::spawn_blocking` to avoid blocking the async runtime.

- [ ] **Step 2: Implement POST /search/scoped handler**

Same pattern with `spawn_blocking` + `scoped::scoped_search`.

- [ ] **Step 3: Implement POST /search/structural handler**

Same pattern with `spawn_blocking` + `structural::structural_search`.

- [ ] **Step 4: Add error handling**

Return proper HTTP status codes:
- 200: success
- 400: bad input (invalid regex, unknown language)
- 500: internal error

- [ ] **Step 5: Test server manually**

```bash
cargo run -- serve --port 8902 &
curl -X POST http://localhost:8902/search -H 'Content-Type: application/json' \
  -d '{"root":"/home/krolik/src/go-code","pattern":"func main","max_results":5}'
curl http://localhost:8902/health
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: implement axum HTTP handlers"
```

---

## Task 7: Docker + deploy

**Files:**
- Create: `~/src/ox-codes/Dockerfile`
- Modify: `~/deploy/krolik-server/docker-compose.yml`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
# Stage 1: Chef
FROM rust:1.93-bookworm AS chef
RUN cargo install cargo-chef --locked
WORKDIR /app

# Stage 2: Planner
FROM chef AS planner
COPY . .
RUN cargo chef prepare --recipe-path recipe.json

# Stage 3: Builder
FROM chef AS builder
COPY --from=planner /app/recipe.json recipe.json
RUN cargo chef cook --release --recipe-path recipe.json
COPY . .
RUN cargo build --release --bin ox-codes

# Stage 4: Runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/ox-codes /usr/local/bin/ox-codes

WORKDIR /app
ENV RUST_LOG=info
EXPOSE 8902

ENTRYPOINT ["ox-codes"]
CMD ["serve"]
```

- [ ] **Step 2: Add ox-codes to docker-compose.yml**

```yaml
ox-codes:
  build:
    context: ../../src/ox-codes
    dockerfile: Dockerfile
  container_name: ox-codes
  ports:
    - "8902:8902"
  volumes:
    - /home/krolik/src:/host-src:ro
  environment:
    - PORT=8902
    - RUST_LOG=info
  restart: unless-stopped
  cap_drop:
    - ALL
  cap_add:
    - DAC_READ_SEARCH
```

- [ ] **Step 3: Build and deploy**

```bash
cd ~/deploy/krolik-server && docker compose build --no-cache ox-codes && docker compose up -d --no-deps --force-recreate ox-codes
```

- [ ] **Step 4: Verify healthcheck**

```bash
curl http://localhost:8902/health
# Expected: "ok"
```

- [ ] **Step 5: Test real search**

```bash
curl -s -X POST http://localhost:8902/search -H 'Content-Type: application/json' \
  -d '{"root":"/host-src/go-code","pattern":"func main","max_results":5}' | jq .
```

- [ ] **Step 6: Commit**

```bash
cd ~/src/ox-codes && git add -A && git commit -m "feat: add Dockerfile and deploy config"
```

---

## Task 8: go-code integration

**Files:**
- Create: `~/src/go-code/internal/oxcodes/client.go`
- Modify: `~/src/go-code/cmd/go-code/tool_code_search.go`
- Modify: `~/deploy/krolik-server/docker-compose.yml` (add OX_CODES_URL to go-code env)

- [ ] **Step 1: Create ox-codes HTTP client in go-code**

```go
// internal/oxcodes/client.go
package oxcodes

type Client struct {
    baseURL    string
    httpClient *http.Client
}

func NewClient(baseURL string) *Client
func (c *Client) Search(ctx context.Context, input SearchInput) ([]SearchMatch, error)
func (c *Client) SearchScoped(ctx context.Context, input ScopedSearchInput) ([]SearchMatch, error)
func (c *Client) SearchStructural(ctx context.Context, input StructuralSearchInput) ([]SearchMatch, error)
```

Types mirror `codesearch.SearchInput`/`SearchMatch` for compatibility.

- [ ] **Step 2: Modify tool_code_search.go for fallback**

In `handleCodeSearch()`:
```go
// Try ox-codes first if configured
if deps.OxCodes != nil {
    result, err := deps.OxCodes.Search(ctx, oxcodesInput)
    if err == nil {
        return formatResult(result), nil
    }
    slog.Warn("ox-codes search failed, falling back to Go", "err", err)
}
// Existing Go codesearch fallback
matches, err := codesearch.Search(ctx, searchInput)
```

- [ ] **Step 3: Add OxCodes to analyze.Deps**

Wire `OX_CODES_URL` env var → `oxcodes.NewClient()` → `deps.OxCodes`.

- [ ] **Step 4: Add new MCP parameters for scoped/structural**

Add `scope` and `structural` fields to `CodeSearchInput`:
- `scope`: `function_bodies`, `comments`, `strings`, `type_definitions`, `imports`
- `structural`: bool — if true, treat pattern as structural (ast-grep)

When `scope` is set → call `oxcodes.SearchScoped()`
When `structural` is set → call `oxcodes.SearchStructural()`

- [ ] **Step 5: Add OX_CODES_URL to docker-compose**

In go-code service environment:
```yaml
- OX_CODES_URL=http://ox-codes:8902
```

- [ ] **Step 6: Build and deploy go-code**

```bash
cd ~/deploy/krolik-server && docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

- [ ] **Step 7: Test end-to-end via MCP**

Use go-code MCP `code_search` tool with a known pattern. Verify it uses ox-codes (check go-code logs for "ox-codes" mention).

- [ ] **Step 8: Commit go-code changes**

```bash
cd ~/src/go-code && git add -A && git commit -m "feat: integrate ox-codes as search backend with fallback"
```

---

## Task 9: Update docs and MEMORY.md

**Files:**
- Modify: `~/CLAUDE.md` (add ox-codes to ports table)
- Modify: `~/.claude/projects/-home-krolik/memory/MEMORY.md` (add ox-codes entry)

- [ ] **Step 1: Update CLAUDE.md ports**

Add `8902 | ox-codes` to ports table. Update "Next free" to 8903+.

- [ ] **Step 2: Update MEMORY.md**

Add ox-codes section with repo path, port, purpose, deploy command.

- [ ] **Step 3: Commit**

```bash
cd ~/src/go-code && git add -A && git commit -m "docs: add ox-codes to project docs"
```
