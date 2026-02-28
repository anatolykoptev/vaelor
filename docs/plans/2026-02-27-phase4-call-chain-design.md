# Phase 4.1: Call Chain Tracing — Design

## Goal

Add call chain tracing to go-code: "What happens when function X is called?" Trace the full execution path across files and packages using tree-sitter AST analysis.

## Scope

- **In scope**: Call extraction via tree-sitter, name-based resolution, call tree building, BFS/DFS tracing, both "callees" and "callers" directions, LLM narrative, new MCP tool `call_trace`
- **Out of scope**: Graph database storage (4.2), cross-language tracing (4.3), type-based resolution (interface dispatch), call frequency analysis

## Architecture

```
Ingest repo → Parse symbols (existing) → Extract calls (new .scm queries)
    → Resolve references (name + import matching) → Build call graph
    → Trace from entry point (BFS, depth=5) → LLM narrative
```

New package: `internal/callgraph/`

### Data Types

```go
// CallSite is a raw call expression extracted from AST.
type CallSite struct {
    Caller   *parser.Symbol // function containing this call
    Name     string         // raw callee name (e.g. "Serve", "fmt.Println")
    Receiver string         // receiver if method call (e.g. "s" in "s.Serve()")
    Line     uint32         // call site line number
}

// CallEdge is a resolved call relationship.
type CallEdge struct {
    Caller     *parser.Symbol
    Callee     *parser.Symbol // nil if unresolved (stdlib, external)
    CalleeName string         // original name from source
    Line       uint32
}

// CallGraph holds all call relationships for a repo.
type CallGraph struct {
    Edges      []CallEdge
    Unresolved []CallSite
    Symbols    []*parser.Symbol // full symbol table for lookup
}

// CallChainNode is one node in a traced call tree.
type CallChainNode struct {
    Symbol   *parser.Symbol
    Children []CallChainNode
    CallLine uint32 // where this call happens in the parent
    Depth    int
}

// TraceResult is the output of a call chain trace.
type TraceResult struct {
    Root       *parser.Symbol
    Tree       []CallChainNode
    MaxDepth   int
    TotalNodes int
    Resolved   int
    Unresolved int
}
```

### Call Extraction (per language)

New `.scm` query patterns capture `@call.function` and `@call.method`:

**Go**:
```scheme
(call_expression function: (identifier) @call.function)
(call_expression function: (selector_expression field: (field_identifier) @call.method))
```

**Python**:
```scheme
(call function: (identifier) @call.function)
(call function: (attribute attribute: (identifier) @call.method))
```

**TypeScript/JS**:
```scheme
(call_expression function: (identifier) @call.function)
(call_expression function: (member_expression property: (property_identifier) @call.method))
```

**Rust**:
```scheme
(call_expression function: (identifier) @call.function)
(call_expression function: (field_expression field: (field_identifier) @call.method))
```

**Java**:
```scheme
(method_invocation name: (identifier) @call.method)
```

**C/C++**:
```scheme
(call_expression function: (identifier) @call.function)
(call_expression function: (field_expression field: (field_identifier) @call.method))
```

**Ruby**:
```scheme
(call method: (identifier) @call.method)
(method_call method: (identifier) @call.function)
```

**C#**:
```scheme
(invocation_expression function: (identifier) @call.function)
(invocation_expression function: (member_access_expression name: (identifier) @call.method))
```

### Resolution Strategy

Three-tier name matching:

1. **Same-file**: call name matches a symbol defined in the same file
2. **Same-package**: call name matches a symbol in a file from the same directory
3. **Cross-package**: `pkg.Func()` → find import path for `pkg` → match symbol in that package's files

Unresolved calls (stdlib, external deps, dynamic dispatch) are kept as leaf nodes with `Callee: nil`.

### Parser Changes

- New `ExtractCalls(path string, source []byte, opts ParseOpts) ([]CallSite, error)` function
- Reuses existing grammar/language setup, runs a separate query for call patterns
- Each `LanguageHandler` gets a new method: `CallsQuery() *sitter.Query` (returns nil if not supported)
- `CallSite.Caller` is resolved by finding which symbol's line range contains the call line

### MCP Tool: `call_trace`

```
Input:
  repo     string  — GitHub URL or local path (required)
  symbol   string  — function/method name to trace (required)
  depth    int     — max trace depth (default 5, max 10)
  direction string — "callees" (default) or "callers"

Output (JSON):
  call_tree   []CallChainNode — recursive call tree
  unresolved  []string        — names of unresolved external calls
  stats       {total_nodes, max_depth, resolved_ratio}
  narrative   string          — LLM explanation of the execution flow
```

### Components

| Component | File | Purpose |
|-----------|------|---------|
| Call queries | `internal/parser/queries/*.scm` | `@call.function` / `@call.method` captures |
| CallsQuery method | `internal/parser/handler_*.go` | Compile and return call query per language |
| Call extractor | `internal/parser/calls.go` | `ExtractCalls()` — run call queries, produce `[]CallSite` |
| Call graph builder | `internal/callgraph/graph.go` | Types + `BuildCallGraph()` from symbols + call sites |
| Resolver | `internal/callgraph/resolve.go` | Name-based resolution (same-file → same-pkg → cross-pkg) |
| Chain tracer | `internal/callgraph/trace.go` | `Trace()` — BFS from entry point with depth limit + cycle detection |
| LLM prompt | `internal/llm/llm.go` | `SystemPromptCallTrace` for narrative generation |
| MCP tool | `cmd/go-code/tool_call_trace.go` | Input validation, orchestration, JSON output |
| Registration | `cmd/go-code/register.go` | Wire `call_trace` tool |

### Cycle Detection

BFS/DFS tracks visited symbols. If a cycle is detected (A -> B -> C -> A), the node is marked as `[cycle]` and not expanded further.

### LLM Narrative

The call tree JSON is sent to the LLM with a system prompt asking for:
- Step-by-step explanation of what happens when the function is called
- Key decision points and error paths
- External calls that leave the repo boundary

Budget: 100K chars for the call tree context.

## Testing Strategy

- Unit tests for each language's call extraction (sample files with known calls)
- Unit tests for resolution (same-file, same-package, cross-package)
- Unit tests for cycle detection
- Integration test: trace a known function in go-code itself
- Edge cases: recursive functions, closures, goroutines, method chains

## Decisions

- **Tree-sitter only** (no golang.org/x/tools) — uniform across 9 languages
- **Name-based resolution** — ~85% accuracy, good enough for practical use
- **Default depth 5** — covers most real scenarios without exponential blowup
- **Both directions** — callees and callers supported
- **JSON + LLM narrative** — best for AI consumers
