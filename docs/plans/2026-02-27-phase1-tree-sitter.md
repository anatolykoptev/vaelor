# Phase 1: Tree-sitter Integration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement tree-sitter AST parsing for Go/Python/TypeScript, improved repo ingestion, and wire the `repo_analyze` + `file_parse` MCP tools into a working pipeline.

**Architecture:** `ingest.IngestRepo()` walks filesystem → `parser.ParseFile()` extracts symbols via tree-sitter → `clean.CleanSource()` strips noise → `analyze.AnalyzeRepo()` orchestrates everything + LLM → MCP tools expose it.

**Tech Stack:** `smacker/go-tree-sitter` (CGo), `modelcontextprotocol/go-sdk` (MCP), Go 1.24

**Current state:** Project scaffolded at `$REPO_ROOT/` with stub implementations (all TODOs). go.mod has no dependencies yet, project does not compile.

---

### Task 1: Resolve dependencies and make the project compile

**Files:**
- Modify: `$REPO_ROOT/go.mod`
- Create: `$REPO_ROOT/go.sum`

**Step 1: Initialize dependencies**

```bash
cd $REPO_ROOT
go get github.com/modelcontextprotocol/go-sdk@latest
go get github.com/smacker/go-tree-sitter@latest
go mod tidy
```

**Step 2: Verify the project compiles**

```bash
cd $REPO_ROOT && CGO_ENABLED=1 go build ./cmd/go-code/
```

Expected: compiles without errors, produces `go-code` binary.

**Step 3: Verify tests pass (trivially)**

```bash
cd $REPO_ROOT && go test ./...
```

Expected: `ok` for each package (no tests yet, but packages must parse).

**Step 4: Commit**

```bash
cd $REPO_ROOT
git add go.mod go.sum
git commit -m "feat: add tree-sitter and MCP SDK dependencies"
```

---

### Task 2: Implement tree-sitter parser — Go language handler

**Files:**
- Rewrite: `$REPO_ROOT/internal/parser/parser.go`
- Modify: `$REPO_ROOT/internal/parser/queries/go.scm` (verify/fix queries)
- Create: `$REPO_ROOT/internal/parser/handler_go.go`
- Create: `$REPO_ROOT/internal/parser/handler.go` (interface + registry)
- Create: `$REPO_ROOT/internal/parser/parser_test.go`
- Create: `$REPO_ROOT/internal/parser/testdata/sample.go`

**Step 1: Create the LanguageHandler interface and registry**

File `internal/parser/handler.go`:

```go
package parser

import (
    sitter "github.com/smacker/go-tree-sitter"
)

// LanguageHandler knows how to parse one programming language via tree-sitter.
type LanguageHandler interface {
    // Language returns the language identifier (e.g. "go", "python").
    Language() string

    // Extensions returns the file extensions handled (e.g. ".go").
    Extensions() []string

    // SitterLanguage returns the tree-sitter grammar.
    SitterLanguage() *sitter.Language

    // TagsQuery returns the compiled tree-sitter query for symbol extraction.
    TagsQuery() *sitter.Query

    // MapCapture interprets a query capture name and node into a Symbol.
    // Returns nil if the capture should be skipped.
    MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol
}

// registry maps file extensions to handlers.
var registry = map[string]LanguageHandler{}

// registerHandler registers a handler for all its extensions.
func registerHandler(h LanguageHandler) {
    for _, ext := range h.Extensions() {
        registry[ext] = h
    }
}

// HandlerForExt returns the handler for the given file extension, or nil.
func HandlerForExt(ext string) LanguageHandler {
    return registry[ext]
}
```

**Step 2: Create the Go language handler**

File `internal/parser/handler_go.go`:

```go
package parser

import (
    _ "embed"
    "strings"

    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/golang"
)

//go:embed queries/go.scm
var goQuerySrc []byte

type goHandler struct {
    lang  *sitter.Language
    query *sitter.Query
}

func init() {
    lang := golang.GetLanguage()
    q, err := sitter.NewQuery(goQuerySrc, lang)
    if err != nil {
        panic("go-tree-sitter: invalid go.scm query: " + err.Error())
    }
    registerHandler(&goHandler{lang: lang, query: q})
}

func (h *goHandler) Language() string           { return "go" }
func (h *goHandler) Extensions() []string       { return []string{".go"} }
func (h *goHandler) SitterLanguage() *sitter.Language { return h.lang }
func (h *goHandler) TagsQuery() *sitter.Query   { return h.query }

func (h *goHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
    // Capture names from go.scm: symbol.function, symbol.method, symbol.type,
    // symbol.const, symbol.var, symbol.name, symbol.params, etc.
    // We only create symbols from the top-level definition captures.
    switch captureName {
    case "symbol.function":
        return extractGoFunction(node, source)
    case "symbol.method":
        return extractGoMethod(node, source)
    case "symbol.type":
        return extractGoType(node, source)
    case "symbol.const":
        return extractGoConst(node, source)
    case "symbol.var":
        return extractGoVar(node, source)
    default:
        return nil
    }
}

// extractGoFunction builds a Symbol from a function_declaration node.
func extractGoFunction(node *sitter.Node, source []byte) *Symbol {
    nameNode := node.ChildByFieldName("name")
    if nameNode == nil {
        return nil
    }
    sym := &Symbol{
        Name:      nameNode.Content(source),
        Kind:      KindFunction,
        Language:  "go",
        StartLine: uint32(node.StartPoint().Row) + 1,
        EndLine:   uint32(node.EndPoint().Row) + 1,
        Signature: extractSignature(node, source),
    }
    return sym
}

// extractGoMethod builds a Symbol from a method_declaration node.
func extractGoMethod(node *sitter.Node, source []byte) *Symbol {
    nameNode := node.ChildByFieldName("name")
    if nameNode == nil {
        return nil
    }
    sym := &Symbol{
        Name:      nameNode.Content(source),
        Kind:      KindMethod,
        Language:  "go",
        StartLine: uint32(node.StartPoint().Row) + 1,
        EndLine:   uint32(node.EndPoint().Row) + 1,
        Signature: extractSignature(node, source),
    }
    return sym
}

// extractGoType builds a Symbol from a type_declaration node.
func extractGoType(node *sitter.Node, source []byte) *Symbol {
    // type_declaration > type_spec > name
    for i := 0; i < int(node.NamedChildCount()); i++ {
        child := node.NamedChild(i)
        if child.Type() == "type_spec" {
            nameNode := child.ChildByFieldName("name")
            if nameNode == nil {
                continue
            }
            kind := KindType
            typeBody := child.ChildByFieldName("type")
            if typeBody != nil {
                switch typeBody.Type() {
                case "struct_type":
                    kind = KindStruct
                case "interface_type":
                    kind = KindInterface
                }
            }
            return &Symbol{
                Name:      nameNode.Content(source),
                Kind:      kind,
                Language:  "go",
                StartLine: uint32(node.StartPoint().Row) + 1,
                EndLine:   uint32(node.EndPoint().Row) + 1,
                Signature: extractSignature(node, source),
            }
        }
    }
    return nil
}

func extractGoConst(node *sitter.Node, source []byte) *Symbol {
    for i := 0; i < int(node.NamedChildCount()); i++ {
        child := node.NamedChild(i)
        if child.Type() == "const_spec" {
            nameNode := child.ChildByFieldName("name")
            if nameNode == nil {
                continue
            }
            return &Symbol{
                Name:      nameNode.Content(source),
                Kind:      KindConst,
                Language:  "go",
                StartLine: uint32(node.StartPoint().Row) + 1,
                EndLine:   uint32(node.EndPoint().Row) + 1,
            }
        }
    }
    return nil
}

func extractGoVar(node *sitter.Node, source []byte) *Symbol {
    for i := 0; i < int(node.NamedChildCount()); i++ {
        child := node.NamedChild(i)
        if child.Type() == "var_spec" {
            nameNode := child.ChildByFieldName("name")
            if nameNode == nil {
                continue
            }
            return &Symbol{
                Name:      nameNode.Content(source),
                Kind:      KindVar,
                Language:  "go",
                StartLine: uint32(node.StartPoint().Row) + 1,
                EndLine:   uint32(node.EndPoint().Row) + 1,
            }
        }
    }
    return nil
}

// extractSignature returns the first line of a node (the declaration line).
func extractSignature(node *sitter.Node, source []byte) string {
    content := node.Content(source)
    if idx := strings.IndexByte(content, '{'); idx > 0 {
        return strings.TrimSpace(content[:idx])
    }
    if idx := strings.IndexByte(content, '\n'); idx > 0 {
        return strings.TrimSpace(content[:idx])
    }
    return content
}
```

**Step 3: Rewrite ParseFile to use tree-sitter**

Rewrite `internal/parser/parser.go` — keep the types (Symbol, ParseResult, ParseOpts, NodeKind), rewrite `ParseFile()`:

```go
func ParseFile(path string, source []byte, opts ParseOpts) (*ParseResult, error) {
    lang := opts.Language
    if lang == "" {
        lang = detectLanguageFromPath(path)
    }

    ext := filepath.Ext(path)
    handler := HandlerForExt(ext)
    if handler == nil {
        return nil, fmt.Errorf("unsupported language for %s", ext)
    }

    parser := sitter.NewParser()
    parser.SetLanguage(handler.SitterLanguage())

    tree, err := parser.ParseCtx(context.Background(), nil, source)
    if err != nil {
        return nil, fmt.Errorf("parse %s: %w", path, err)
    }

    root := tree.RootNode()
    result := &ParseResult{
        File:     path,
        Language: handler.Language(),
    }

    if root.HasError() {
        result.Error = fmt.Errorf("parse errors in %s", path)
    }

    // Run tags query to extract symbols.
    qc := sitter.NewQueryCursor()
    qc.Exec(handler.TagsQuery(), root)

    seen := map[string]bool{} // dedup by "kind:name:line"
    for {
        match, ok := qc.NextMatch()
        if !ok {
            break
        }
        for _, capture := range match.Captures {
            captureName := handler.TagsQuery().CaptureNameForId(capture.Index)

            // Handle import captures separately.
            if captureName == "import.path" {
                importPath := strings.Trim(capture.Node.Content(source), "\"")
                result.Imports = append(result.Imports, importPath)
                continue
            }

            sym := handler.MapCapture(captureName, capture.Node, source)
            if sym == nil {
                continue
            }

            key := fmt.Sprintf("%s:%s:%d", sym.Kind, sym.Name, sym.StartLine)
            if seen[key] {
                continue
            }
            seen[key] = true

            sym.File = path
            if opts.IncludeBody {
                sym.Body = capture.Node.Content(source)
            }
            result.Symbols = append(result.Symbols, sym)
        }
    }

    return result, nil
}
```

Note: `ParseFile` now takes `source []byte` parameter instead of reading from disk — the caller (ingest) reads the file and passes bytes. This is cleaner and more testable.

**Step 4: Create test data and test**

File `internal/parser/testdata/sample.go`:
```go
package sample

import (
    "fmt"
    "net/http"
)

// MaxRetries is the maximum number of retries.
const MaxRetries = 3

// Config holds the configuration.
type Config struct {
    Host string
    Port int
}

// Handler is the HTTP handler interface.
type Handler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}

var defaultConfig = Config{Host: "localhost", Port: 8080}

// NewConfig creates a new Config with defaults.
func NewConfig() *Config {
    return &defaultConfig
}

// Run starts the server.
func (c *Config) Run() error {
    addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
    return http.ListenAndServe(addr, nil)
}
```

File `internal/parser/parser_test.go`:
```go
package parser_test

import (
    "os"
    "testing"

    "github.com/anatolykoptev/go-code/internal/parser"
)

func TestParseGoFile(t *testing.T) {
    source, err := os.ReadFile("testdata/sample.go")
    if err != nil {
        t.Fatal(err)
    }

    result, err := parser.ParseFile("testdata/sample.go", source, parser.ParseOpts{IncludeBody: true})
    if err != nil {
        t.Fatal(err)
    }

    if result.Language != "go" {
        t.Errorf("language = %q, want go", result.Language)
    }

    // Check imports.
    wantImports := []string{"fmt", "net/http"}
    if len(result.Imports) != len(wantImports) {
        t.Errorf("imports count = %d, want %d: %v", len(result.Imports), len(wantImports), result.Imports)
    }

    // Check we found the expected symbols.
    wantSymbols := map[string]parser.NodeKind{
        "MaxRetries":    parser.KindConst,
        "Config":        parser.KindStruct,
        "Handler":       parser.KindInterface,
        "defaultConfig": parser.KindVar,
        "NewConfig":     parser.KindFunction,
        "Run":           parser.KindMethod,
    }

    found := map[string]parser.NodeKind{}
    for _, sym := range result.Symbols {
        found[sym.Name] = sym.Kind
    }

    for name, wantKind := range wantSymbols {
        gotKind, ok := found[name]
        if !ok {
            t.Errorf("symbol %q not found", name)
            continue
        }
        if gotKind != wantKind {
            t.Errorf("symbol %q kind = %q, want %q", name, gotKind, wantKind)
        }
    }

    // Check signatures are non-empty for functions/methods.
    for _, sym := range result.Symbols {
        if (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) && sym.Signature == "" {
            t.Errorf("symbol %q (%s) has empty signature", sym.Name, sym.Kind)
        }
    }
}

func TestDetectLanguage(t *testing.T) {
    tests := []struct {
        path string
        want string
    }{
        {"main.go", "go"},
        {"app.py", "python"},
        {"index.ts", "typescript"},
        {"script.js", "javascript"},
        {"lib.rs", "rust"},
        {"Main.java", "java"},
        {"unknown.xyz", ""},
    }
    for _, tt := range tests {
        got := parser.DetectLanguageFromPath(tt.path)
        if got != tt.want {
            t.Errorf("DetectLanguageFromPath(%q) = %q, want %q", tt.path, got, tt.want)
        }
    }
}
```

**Step 5: Run tests**

```bash
cd $REPO_ROOT && CGO_ENABLED=1 go test ./internal/parser/ -v
```

Expected: PASS — all symbols extracted, imports found, signatures non-empty.

**Step 6: Commit**

```bash
cd $REPO_ROOT
git add internal/parser/
git commit -m "feat(parser): implement tree-sitter Go parsing with symbol extraction"
```

---

### Task 3: Add Python and TypeScript language handlers

**Files:**
- Create: `$REPO_ROOT/internal/parser/handler_python.go`
- Create: `$REPO_ROOT/internal/parser/handler_typescript.go`
- Modify: `$REPO_ROOT/internal/parser/queries/python.scm` (verify/fix)
- Modify: `$REPO_ROOT/internal/parser/queries/typescript.scm` (verify/fix)
- Create: `$REPO_ROOT/internal/parser/testdata/sample.py`
- Create: `$REPO_ROOT/internal/parser/testdata/sample.ts`
- Modify: `$REPO_ROOT/internal/parser/parser_test.go`

**Step 1: Implement Python handler**

Same pattern as Go handler but using `github.com/smacker/go-tree-sitter/python` grammar.
Extract: functions (`def`), classes, methods (def inside class), imports.
Query captures: `symbol.function`, `symbol.class`, `import.path`.

**Step 2: Implement TypeScript handler**

Using `github.com/smacker/go-tree-sitter/typescript/typescript` grammar.
Extract: functions, arrow functions, classes, interfaces, imports.
Query captures: `symbol.function`, `symbol.class`, `symbol.interface`, `import.path`.

**Step 3: Create test data**

`testdata/sample.py` — file with class, functions, imports.
`testdata/sample.ts` — file with interface, class, functions, imports.

**Step 4: Add tests**

`TestParsePythonFile` and `TestParseTypeScriptFile` in `parser_test.go`.

**Step 5: Run tests**

```bash
cd $REPO_ROOT && CGO_ENABLED=1 go test ./internal/parser/ -v -count=1
```

**Step 6: Commit**

```bash
git add internal/parser/
git commit -m "feat(parser): add Python and TypeScript language handlers"
```

---

### Task 4: Implement repository ingestion

**Files:**
- Rewrite: `$REPO_ROOT/internal/ingest/ingest.go`
- Create: `$REPO_ROOT/internal/ingest/ignore.go` (gitignore + defaults)
- Create: `$REPO_ROOT/internal/ingest/tree.go` (file tree rendering)
- Create: `$REPO_ROOT/internal/ingest/ingest_test.go`
- Create: `$REPO_ROOT/internal/ingest/testdata/` (test fixtures)

**Step 1: Implement ignore rules**

File `internal/ingest/ignore.go`:
- Default ignored dirs: `.git`, `node_modules`, `vendor`, `__pycache__`, `dist`, `build`, `.next`, `.venv`, `.idea`, `.vscode`, etc.
- Binary extensions: `.exe`, `.dll`, `.so`, `.png`, `.jpg`, `.mp4`, etc.
- Parse `.gitignore` from repo root.
- `shouldIgnoreDir(name)`, `shouldIgnoreFile(name, ext)`, `matchGitignore(relPath, patterns)`.

**Step 2: Implement IngestRepo**

Rewrite `internal/ingest/ingest.go`:
- `filepath.WalkDir` with early `SkipDir` for ignored dirs.
- Symlink detection and skip.
- Max depth limit (20).
- Max files limit (10_000).
- Language detection via `parser.DetectLanguageFromPath`.
- Read file content, check for binary data (null bytes in first 512 bytes).
- Size filtering (MaxFileBytes from opts).
- Build `IngestResult` with `[]*File` populated.

**Step 3: Implement file tree rendering**

File `internal/ingest/tree.go`:
- `RenderTree(root string, files []*File) string`
- Box-drawing tree: `├──`, `└──`, `│   `.
- Max 100 lines, truncate with `... (N more)`.

**Step 4: Create test fixtures and tests**

`internal/ingest/testdata/` — small fake repo with:
- `main.go`, `internal/handler.go`, `go.mod`
- `.gitignore` with `*.log`
- `test.log` (should be ignored)
- `node_modules/foo.js` (dir should be ignored)
- `binary.exe` (should be skipped as binary ext)

Tests:
- `TestIngestRepo` — verify correct files found, ignored files excluded.
- `TestRenderTree` — verify tree output format.
- `TestShouldIgnoreDir` — test default ignore list.

**Step 5: Run tests**

```bash
cd $REPO_ROOT && go test ./internal/ingest/ -v
```

**Step 6: Commit**

```bash
git add internal/ingest/
git commit -m "feat(ingest): implement filesystem walk with gitignore and binary detection"
```

---

### Task 5: Implement smart cleaning

**Files:**
- Rewrite: `$REPO_ROOT/internal/clean/clean.go`
- Create: `$REPO_ROOT/internal/clean/comments.go` (per-language comment stripping)
- Create: `$REPO_ROOT/internal/clean/clean_test.go`

**Step 1: Implement per-language comment stripping**

File `internal/clean/comments.go`:
- C-style (`//`, `/* */`): Go, JS, TS, Java, Rust, C, C++, C#, Swift, Kotlin
- Hash (`#`): Python, Ruby, Shell, YAML, TOML
- Preserve: lines with `TODO`, `FIXME`, `HACK`, `NOTE`, `BUG`, `nolint`, `eslint-disable`
- Preserve: doc comments (`///`, `/** */`, `"""..."""`)

**Step 2: Implement three cleaning modes**

In `clean.go`:
- `ModeSignatures` — extract function/type signatures only (uses parser.Symbol data)
- `ModeSkeleton` — keep structure, replace function bodies with `...`
- `ModeFull` — full content with comment stripping + blank line collapsing
- `ModeRaw` — no cleaning (passthrough)

**Step 3: Tests**

Test each mode with Go and Python samples. Verify comments removed, structure preserved, signatures extracted.

**Step 4: Commit**

```bash
git add internal/clean/
git commit -m "feat(clean): implement per-language comment stripping and cleaning modes"
```

---

### Task 6: Wire up the analysis pipeline

**Files:**
- Rewrite: `$REPO_ROOT/internal/analyze/analyze.go`
- Create: `$REPO_ROOT/internal/analyze/context.go` (LLM context builder)
- Create: `$REPO_ROOT/internal/analyze/analyze_test.go`

**Step 1: Implement AnalyzeRepo**

```go
func AnalyzeRepo(ctx context.Context, input RepoAnalysisInput, deps Deps) (*RepoAnalysisResult, error) {
    // 1. Ingest: walk filesystem, collect files
    ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
        Root:         input.Root,
        Focus:        input.Focus,
        MaxFileBytes: deps.MaxFileBytes,
    })

    // 2. Parse: extract symbols from each file via tree-sitter
    parseResults := parseFilesParallel(ctx, ingestResult.Files)

    // 3. Clean: prepare code for LLM context
    context := buildLLMContext(ingestResult, parseResults, input.Query)

    // 4. LLM: answer the query
    answer, err := deps.LLM.Complete(ctx, llm.SystemPromptRepoAnalysis, context)

    // 5. Build result
    return &RepoAnalysisResult{...}, nil
}
```

`Deps` struct holds injected dependencies: LLM client, max file size, etc.

**Step 2: Implement LLM context builder**

File `internal/analyze/context.go`:
- Build structured context: file tree + symbol summaries + selected file contents.
- Prioritize files by: focus match > import frequency > git change frequency.
- Budget: max 150_000 chars for LLM context.
- Format: `=== File: path ===\n<content>` blocks.

**Step 3: Implement SearchSymbols**

- Ingest + parse all files.
- Filter symbols by name pattern (wildcard `*` support) and kind.
- Return matched symbols.

**Step 4: Implement BuildDepGraph**

- Ingest + parse all files → collect imports.
- Build adjacency list: package → imports.
- Render in requested format (mermaid, dot, json).

**Step 5: Tests**

Integration test with a small fixture repo. Test AnalyzeRepo with a mock LLM.

**Step 6: Commit**

```bash
git add internal/analyze/
git commit -m "feat(analyze): wire ingest → parser → clean → llm pipeline"
```

---

### Task 7: Wire MCP tool handlers

**Files:**
- Rewrite: `$REPO_ROOT/cmd/go-code/tool_repo_analyze.go`
- Rewrite: `$REPO_ROOT/cmd/go-code/tool_file_parse.go`
- Modify: `$REPO_ROOT/cmd/go-code/register.go`
- Modify: `$REPO_ROOT/cmd/go-code/main.go` (init workspace dir)

**Step 1: Wire repo_analyze handler**

Replace TODO stub with:
1. Determine if input is remote (clone) or local (use directly).
2. Call `analyze.AnalyzeRepo()` with deps.
3. Format output as structured text.
4. Return `mcp.CallToolResult`.

**Step 2: Wire file_parse handler**

1. Read file from disk.
2. Call `parser.ParseFile()`.
3. Format as symbols list or raw AST depending on `output_format`.
4. Return result.

**Step 3: Update register.go**

Pass `analyze.Deps` with LLM client and config values from `Config`.

**Step 4: Test manually**

```bash
cd $REPO_ROOT && CGO_ENABLED=1 go build -o bin/go-code ./cmd/go-code
LLM_API_KEY=$LLM_API_KEY ./bin/go-code
```

In another terminal, test with curl or MCP client.

**Step 5: Commit**

```bash
git add cmd/go-code/
git commit -m "feat: wire repo_analyze and file_parse MCP tool handlers"
```

---

### Task 8: Build, lint, test, deploy

**Step 1: Run linter**

```bash
cd $REPO_ROOT && make lint
```

Fix any issues.

**Step 2: Run all tests**

```bash
cd $REPO_ROOT && CGO_ENABLED=1 make test
```

**Step 3: Build Docker image**

```bash
cd $REPO_ROOT && docker build -t go-code:test .
```

**Step 4: Add to docker-compose**

Add `go-code` service to `/home/user/deploy/my-server/docker-compose.yml`.

**Step 5: Deploy**

```bash
cd /home/user/deploy/my-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
```

**Step 6: Register MCP**

```bash
claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp
```

**Step 7: Verify**

Test `repo_analyze` and `file_parse` tools via Claude Code.

**Step 8: Commit all remaining changes**

```bash
git add -A
git commit -m "feat: Phase 1 complete — tree-sitter parsing, ingestion, analysis pipeline"
```

---

## Task Dependencies

```
Task 1 (deps) → Task 2 (Go parser) → Task 3 (Python/TS parsers)
                                            ↓
Task 4 (ingestion) ─────────────────→ Task 6 (pipeline)
                                            ↑
Task 5 (cleaning) ──────────────────────────┘
                                            ↓
                                     Task 7 (MCP tools) → Task 8 (deploy)
```

Tasks 2-5 can be parallelized after Task 1 completes.
Task 6 requires Tasks 2-5.
Tasks 7-8 are sequential after Task 6.
