# API Consistency Improvements — Design

Date: 2026-03-03

## Problem

LLMs frequently misuse go-code tool parameters due to naming inconsistencies:
- `file_parse` doesn't accept `repo` — can't parse files from GitHub repos
- `code_search` uses `file_glob` but LLMs guess `path`
- `dep_graph` uses `max_depth` while `call_trace`/`impact_analysis` use `depth`
- `symbol_search` uses `query` while `call_trace`/`impact_analysis` use `symbol`
- Validation errors don't suggest valid parameters

## Changes

### 1. `file_parse` — add `repo` + `ref` support

**File**: `cmd/go-code/tool_file_parse.go`

Add `repo` and `ref` optional params. When `repo` present, use `resolveRoot()` (same as `repo_analyze`) to clone/cache, then join root + path.

```go
type FileParseInput struct {
    Repo         string `json:"repo,omitempty"`
    Ref          string `json:"ref,omitempty"`
    Path         string `json:"path"`
    Language     string `json:"language,omitempty"`
    OutputFormat string `json:"output_format,omitempty"`
}
```

Logic:
- `repo` present → `resolveRoot(ctx, repo, ref, deps)` → `filepath.Join(root, path)`
- `repo` absent → current behavior (local path, rewritePath)

Switch from `mcp.AddTool` to `mcpserver.AddTool` for string coercion consistency.
Handler needs `deps` parameter (like `repo_analyze`).

### 2. `code_search` — add `path` alias for `file_glob`

**File**: `cmd/go-code/tool_code_search.go`

```go
type CodeSearchInput struct {
    // ... existing fields ...
    Path string `json:"path,omitempty"` // alias for file_glob
}
```

In handler, before search:
```go
if in.Path != "" && in.FileGlob == "" {
    in.FileGlob = in.Path + "/**"
}
```

### 3. `dep_graph` — rename `max_depth` → `depth`

**File**: `cmd/go-code/tool_dep_graph.go`

```go
type DepGraphInput struct {
    // ... existing fields ...
    Depth    int `json:"depth,omitempty"`
    MaxDepth int `json:"max_depth,omitempty"` // deprecated alias
}
```

In handler:
```go
if in.MaxDepth > 0 && in.Depth == 0 {
    in.Depth = in.MaxDepth
}
```

Update tool description to use `depth`. Keep `max_depth` as deprecated alias.

### 4. Better validation errors in go-mcpserver

**File**: `vendor/github.com/anatolykoptev/go-mcpserver/lenient.go`

When JSON schema validation fails with "unexpected additional properties":
- Extract the unknown property names from the error
- List valid properties from the schema
- Return: `unknown parameter "path". Valid parameters: repo, pattern, is_regex, file_glob, ...`

### 5. `symbol_search` — add `symbol` alias for `query`

**File**: `cmd/go-code/tool_symbol_search.go`

```go
type SymbolSearchInput struct {
    // ... existing fields ...
    Symbol string `json:"symbol,omitempty"` // alias for query
}
```

In handler:
```go
if in.Symbol != "" && in.Query == "" {
    in.Query = in.Symbol
}
```

## Scope

- 5 files in go-code changed
- 1 file in go-mcpserver (vendored) changed
- No new dependencies
- Backward compatible (all changes are additive aliases)
