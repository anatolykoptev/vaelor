# Phase 4.1: Call Chain Tracing — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `call_trace` MCP tool that traces execution paths through a codebase — "what happens when function X is called?"

**Architecture:** Extend tree-sitter `.scm` queries to capture `call_expression` nodes. A new `internal/callgraph/` package resolves calls against the symbol table (same-file → same-package → cross-package), builds a directed call graph, and traces BFS paths from an entry point. LLM generates a narrative explanation of the execution flow.

**Tech Stack:** tree-sitter (existing), Go stdlib, CLIProxyAPI/Gemini LLM (existing)

---

### Task 1: Add Call Extraction Queries (.scm files)

**Files:**
- Modify: `internal/parser/queries/go.scm`
- Modify: `internal/parser/queries/python.scm`
- Modify: `internal/parser/queries/typescript.scm`
- Modify: `internal/parser/queries/rust.scm`
- Modify: `internal/parser/queries/java.scm`
- Modify: `internal/parser/queries/c.scm`
- Modify: `internal/parser/queries/cpp.scm`
- Modify: `internal/parser/queries/ruby.scm`
- Modify: `internal/parser/queries/csharp.scm`

**Context:** Each `.scm` file currently has `@symbol.*` and `@import.*` captures for declarations. We add `@call.function` and `@call.method` captures for call expressions. These are separate query patterns appended to the end of each file.

**Step 1: Add Go call queries**

Append to `internal/parser/queries/go.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Method calls: obj.Method()
(call_expression
  function: (selector_expression
    field: (field_identifier) @call.method))
```

**Step 2: Add Python call queries**

Append to `internal/parser/queries/python.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call
  function: (identifier) @call.function)

; Method calls: obj.method()
(call
  function: (attribute
    attribute: (identifier) @call.method))
```

**Step 3: Add TypeScript/JS call queries**

Append to `internal/parser/queries/typescript.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Method calls: obj.method()
(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))
```

**Step 4: Add Rust call queries**

Append to `internal/parser/queries/rust.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Method calls: obj.method()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

**Step 5: Add Java call queries**

Append to `internal/parser/queries/java.scm`:

```scheme
; --- Call extraction ---

; Method invocations: obj.method() or method()
(method_invocation
  name: (identifier) @call.method)
```

**Step 6: Add C call queries**

Append to `internal/parser/queries/c.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Member function calls via pointer: obj->method()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

**Step 7: Add C++ call queries**

Append to `internal/parser/queries/cpp.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Method calls: obj.method() or obj->method()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

**Step 8: Add Ruby call queries**

Append to `internal/parser/queries/ruby.scm`:

```scheme
; --- Call extraction ---

; Method calls: obj.method()
(call
  method: (identifier) @call.method)
```

**Step 9: Add C# call queries**

Append to `internal/parser/queries/csharp.scm`:

```scheme
; --- Call extraction ---

; Direct function calls: Foo()
(invocation_expression
  function: (identifier) @call.function)

; Method calls: obj.Method()
(invocation_expression
  function: (member_access_expression
    name: (identifier) @call.method))
```

**Step 10: Verify queries compile**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestParse -count=1 -v 2>&1 | tail -20`
Expected: All existing parser tests PASS. If a `.scm` query has a syntax error, the handler's `init()` panics with "query compile error".

**Step 11: Commit**

```bash
git add internal/parser/queries/
git commit -m "feat(parser): add call expression queries for all 9 languages"
```

---

### Task 2: Extend LanguageHandler Interface + Call Extraction

**Files:**
- Modify: `internal/parser/handler.go` (add capture constants + optional interface)
- Create: `internal/parser/calls.go` (ExtractCalls function)
- Create: `internal/parser/calls_test.go` (tests)
- Modify: `internal/parser/handler_go.go` (implement CallsQuery)
- Modify: `internal/parser/handler_python.go` (implement CallsQuery)
- Modify: `internal/parser/handler_typescript.go` (implement CallsQuery)
- Modify: `internal/parser/handler_rust.go` (implement CallsQuery)
- Modify: `internal/parser/handler_java.go` (implement CallsQuery)
- Modify: `internal/parser/handler_c.go` (implement CallsQuery)
- Modify: `internal/parser/handler_cpp.go` (implement CallsQuery)
- Modify: `internal/parser/handler_ruby.go` (implement CallsQuery)
- Modify: `internal/parser/handler_csharp.go` (implement CallsQuery)

**Context:** We add a new optional interface `CallQueryProvider` instead of modifying the existing `LanguageHandler`. Each handler compiles its call queries in `init()` alongside its existing `TagsQuery`. A new `ExtractCalls()` function runs the call query and produces raw call sites.

**Step 1: Write the failing test**

Create `internal/parser/calls_test.go`:

```go
package parser

import (
	"testing"
)

func TestExtractCalls_Go(t *testing.T) {
	source := []byte(`package main

import "fmt"

func helper() int {
	return 42
}

func main() {
	x := helper()
	fmt.Println(x)
	s := &Server{}
	s.Start()
}
`)
	calls, err := ExtractCalls("main.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	// Expect: helper (function), Println (method), Start (method)
	if len(calls) < 3 {
		t.Fatalf("got %d calls, want >= 3", len(calls))
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"helper", "Println", "Start"} {
		if !found[want] {
			t.Errorf("missing call to %q in extracted calls", want)
		}
	}
}

func TestExtractCalls_Python(t *testing.T) {
	source := []byte(`
def helper():
    return 42

def main():
    x = helper()
    print(x)
    obj.process()
`)
	calls, err := ExtractCalls("main.py", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"helper", "print", "process"} {
		if !found[want] {
			t.Errorf("missing call to %q", want)
		}
	}
}

func TestExtractCalls_Unsupported(t *testing.T) {
	// .txt has no handler — should return empty, no error
	calls, err := ExtractCalls("readme.txt", []byte("hello"), ParseOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("got %d calls for unsupported file, want 0", len(calls))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestExtractCalls -v -count=1`
Expected: FAIL — `ExtractCalls` undefined.

**Step 3: Add call capture constants and CallQueryProvider interface**

Add to `internal/parser/handler.go` after the existing capture constants:

```go
const (
	captureCallFunction = "call.function"
	captureCallMethod   = "call.method"
)

// CallQueryProvider is an optional interface that LanguageHandler implementations
// can satisfy to support call extraction. Handlers that don't implement this
// interface simply don't support call tracing.
type CallQueryProvider interface {
	// CallsQuery returns the compiled tree-sitter query for call extraction.
	// Returns nil if call extraction is not supported.
	CallsQuery() *sitter.Query
}
```

**Step 4: Add CallSite type and ExtractCalls function**

Create `internal/parser/calls.go`:

```go
package parser

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// CallSite represents a single function/method call extracted from source code.
type CallSite struct {
	// Name is the called function or method name (e.g. "Println", "helper").
	Name string

	// Receiver is the receiver/qualifier for method calls (e.g. "fmt" in "fmt.Println").
	// Empty for plain function calls.
	Receiver string

	// Line is the 1-based line number of the call site.
	Line uint32

	// File is the absolute path of the source file containing this call.
	File string
}

// ExtractCalls parses a source file and returns all function/method call sites.
// Returns an empty slice (not error) for unsupported languages.
func ExtractCalls(path string, source []byte, opts ParseOpts) ([]CallSite, error) {
	lang := opts.Language
	if lang == "" {
		lang = DetectLanguageFromPath(path)
	}
	if lang == "" {
		return nil, nil
	}

	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	cqp, ok := handler.(CallQueryProvider)
	if !ok || cqp.CallsQuery() == nil {
		return nil, nil
	}

	p := sitter.NewParser()
	p.SetLanguage(handler.SitterLanguage())

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}

	return runCallQuery(cqp.CallsQuery(), tree.RootNode(), source, path), nil
}

// runCallQuery executes the call query against the tree and produces CallSite entries.
func runCallQuery(q *sitter.Query, root *sitter.Node, source []byte, path string) []CallSite {
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)

	var calls []CallSite
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			capName := q.CaptureNameForId(capture.Index)
			node := capture.Node
			name := node.Content(source)

			cs := CallSite{
				Name: name,
				Line: node.StartPoint().Row + 1,
				File: path,
			}

			if capName == captureCallMethod {
				// For method calls, try to extract the receiver from parent.
				cs.Receiver = extractCallReceiver(node, source)
			}

			calls = append(calls, cs)
		}
	}

	return calls
}

// extractCallReceiver tries to extract the receiver/object name from a method call node.
// For "obj.Method()", returns "obj". For "pkg.Func()", returns "pkg".
func extractCallReceiver(methodNode *sitter.Node, source []byte) string {
	// The method node is the field/property identifier inside a selector/member expression.
	// Its parent should be the selector expression, whose first child is the object.
	parent := methodNode.Parent()
	if parent == nil {
		return ""
	}

	// In Go: selector_expression has operand + field
	// In Python: attribute has object + attribute
	// In TS/JS: member_expression has object + property
	// The object is typically the first named child or ChildByFieldName("operand"/"object")
	for _, fieldName := range []string{"operand", "object"} {
		obj := parent.ChildByFieldName(fieldName)
		if obj != nil {
			text := obj.Content(source)
			// Take only the last identifier segment for chains: a.b.c -> c
			if idx := strings.LastIndexByte(text, '.'); idx >= 0 {
				return text[idx+1:]
			}
			return text
		}
	}

	// Fallback: first named child
	if parent.NamedChildCount() > 0 {
		first := parent.NamedChild(0)
		if first != nil && first != methodNode {
			text := first.Content(source)
			if idx := strings.LastIndexByte(text, '.'); idx >= 0 {
				return text[idx+1:]
			}
			return text
		}
	}

	return ""
}
```

**Step 5: Implement CallsQuery on all handlers**

Each handler needs a second query compiled from the same `.scm` file but only matching call patterns. The approach: compile a separate query from just the call patterns.

However, since `.scm` files are single files with both symbol and call queries, we need to split them. **Simpler approach**: embed a separate `queries/go_calls.scm` file per language.

**Alternative (simpler, recommended)**: Use the same `.scm` file but compile two queries — one for symbols (existing `TagsQuery`), one for calls (`CallsQuery`). Since tree-sitter queries can be concatenated, append call patterns to each `.scm`. Then compile separate queries from separate embedded byte slices.

**Simplest approach**: Create separate `*_calls.scm` files.

Create `internal/parser/queries/go_calls.scm`:
```scheme
(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (selector_expression
    field: (field_identifier) @call.method))
```

Create `internal/parser/queries/python_calls.scm`:
```scheme
(call
  function: (identifier) @call.function)

(call
  function: (attribute
    attribute: (identifier) @call.method))
```

Create `internal/parser/queries/typescript_calls.scm`:
```scheme
(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))
```

Create `internal/parser/queries/rust_calls.scm`:
```scheme
(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

Create `internal/parser/queries/java_calls.scm`:
```scheme
(method_invocation
  name: (identifier) @call.method)
```

Create `internal/parser/queries/c_calls.scm`:
```scheme
(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

Create `internal/parser/queries/cpp_calls.scm`:
```scheme
(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))
```

Create `internal/parser/queries/ruby_calls.scm`:
```scheme
(call
  method: (identifier) @call.method)
```

Create `internal/parser/queries/csharp_calls.scm`:
```scheme
(invocation_expression
  function: (identifier) @call.function)

(invocation_expression
  function: (member_access_expression
    name: (identifier) @call.method))
```

**Then update each handler** to embed and compile the calls query. Example for Go (`handler_go.go`):

Add to the existing embed/init block:

```go
//go:embed queries/go_calls.scm
var goCallsQueryBytes []byte
```

Add field to `goHandler`:
```go
type goHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}
```

Add to `init()`:
```go
cq, err := sitter.NewQuery(goCallsQueryBytes, lang)
if err != nil {
	panic("go_calls.scm query compile error: " + err.Error())
}
goLang.callQuery = cq
```

Add method:
```go
func (h *goHandler) CallsQuery() *sitter.Query { return h.callQuery }
```

**Repeat for all 9 handlers.** Same pattern: embed `*_calls.scm`, compile in `init()`, add `CallsQuery()` method.

**Step 6: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestExtractCalls -v -count=1`
Expected: PASS — all 3 test functions pass.

Also verify existing tests still pass:
Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -count=1`
Expected: All PASS.

**Step 7: Commit**

```bash
git add internal/parser/
git commit -m "feat(parser): add call extraction via CallsQuery for all 9 languages"
```

---

### Task 3: Call Graph Types and Builder

**Files:**
- Create: `internal/callgraph/graph.go` (types + BuildCallGraph)
- Create: `internal/callgraph/graph_test.go` (tests)

**Context:** The callgraph package takes parsed symbols + extracted call sites and builds a directed graph of resolved call edges. Resolution is name-based: same-file → same-package → cross-package via import mapping.

**Step 1: Write the failing test**

Create `internal/callgraph/graph_test.go`:

```go
package callgraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestBuildCallGraph_SameFile(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 10, EndLine: 20},
		{Name: "helper", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 1, EndLine: 8},
	}
	calls := []parser.CallSite{
		{Name: "helper", File: "/repo/main.go", Line: 15},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(g.Edges))
	}

	edge := g.Edges[0]
	if edge.Caller.Name != "main" {
		t.Errorf("caller = %s, want main", edge.Caller.Name)
	}
	if edge.Callee == nil || edge.Callee.Name != "helper" {
		t.Errorf("callee = %v, want helper", edge.Callee)
	}
}

func TestBuildCallGraph_CrossPackage(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/repo/cmd/main.go", StartLine: 5, EndLine: 15},
		{Name: "Serve", Kind: parser.KindMethod, File: "/repo/internal/server/server.go", StartLine: 10, EndLine: 30},
	}
	calls := []parser.CallSite{
		{Name: "Serve", Receiver: "s", File: "/repo/cmd/main.go", Line: 12},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(g.Edges))
	}
	if g.Edges[0].Callee == nil || g.Edges[0].Callee.Name != "Serve" {
		t.Errorf("callee = %v, want Serve", g.Edges[0].Callee)
	}
}

func TestBuildCallGraph_Unresolved(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 1, EndLine: 10},
	}
	calls := []parser.CallSite{
		{Name: "Println", Receiver: "fmt", File: "/repo/main.go", Line: 5},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(g.Edges))
	}
	if g.Edges[0].Callee != nil {
		t.Errorf("callee should be nil (unresolved stdlib call)")
	}
	if g.Edges[0].CalleeName != "Println" {
		t.Errorf("calleeName = %s, want Println", g.Edges[0].CalleeName)
	}
}

func TestBuildCallGraph_FindCaller(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "outer", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 1, EndLine: 20},
		{Name: "inner", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 5, EndLine: 8},
	}
	calls := []parser.CallSite{
		{Name: "Println", File: "/repo/main.go", Line: 15}, // inside outer (line 1-20), outside inner (5-8)
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(g.Edges))
	}
	if g.Edges[0].Caller.Name != "outer" {
		t.Errorf("caller = %s, want outer (narrowest containing function)", g.Edges[0].Caller.Name)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestBuildCallGraph -v -count=1`
Expected: FAIL — package doesn't exist.

**Step 3: Implement CallGraph types and BuildCallGraph**

Create `internal/callgraph/graph.go`:

```go
// Package callgraph builds and queries call relationships between functions.
//
// It takes parsed symbols and extracted call sites from the parser package,
// resolves call targets by name matching (same-file → same-package → cross-package),
// and builds a directed call graph suitable for tracing execution paths.
package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// CallEdge is a resolved (or unresolved) call from one function to another.
type CallEdge struct {
	// Caller is the function containing the call expression.
	Caller *parser.Symbol

	// Callee is the resolved target function. Nil if unresolved (stdlib, external).
	Callee *parser.Symbol

	// CalleeName is the original name from source (always populated).
	CalleeName string

	// Receiver is the qualifier/receiver if this is a method call.
	Receiver string

	// Line is the 1-based line number of the call site.
	Line uint32
}

// CallGraph holds all call relationships for a repository.
type CallGraph struct {
	// Edges is all call edges (both resolved and unresolved).
	Edges []CallEdge

	// Symbols is the full symbol table used for resolution.
	Symbols []*parser.Symbol
}

// BuildCallGraph resolves call sites against the symbol table and returns a CallGraph.
//
// Resolution strategy (first match wins):
//  1. Same file: call name matches a function/method defined in the same file.
//  2. Same package: call name matches a function/method in the same directory.
//  3. Global: call name matches any function/method in the symbol table.
//
// Unresolved calls (stdlib, external deps) get Callee = nil.
func BuildCallGraph(symbols []*parser.Symbol, calls []parser.CallSite) *CallGraph {
	// Index: name → symbols (multiple symbols can share a name across files).
	byName := indexByName(symbols)

	// Index: file → []*Symbol (for same-file resolution).
	byFile := indexByFile(symbols)

	// Index: dir → []*Symbol (for same-package resolution).
	byDir := indexByDir(symbols)

	edges := make([]CallEdge, 0, len(calls))

	for i := range calls {
		cs := &calls[i]

		caller := findCaller(byFile[cs.File], cs.Line)

		callee := resolveCall(cs, byFile, byDir, byName)

		edges = append(edges, CallEdge{
			Caller:     caller,
			Callee:     callee,
			CalleeName: cs.Name,
			Receiver:   cs.Receiver,
			Line:       cs.Line,
		})
	}

	return &CallGraph{
		Edges:   edges,
		Symbols: symbols,
	}
}

// findCaller returns the narrowest function/method symbol that contains the given line.
func findCaller(fileSymbols []*parser.Symbol, line uint32) *parser.Symbol {
	var best *parser.Symbol
	for _, sym := range fileSymbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if line >= sym.StartLine && line <= sym.EndLine {
			if best == nil || (sym.EndLine-sym.StartLine) < (best.EndLine-best.StartLine) {
				best = sym
			}
		}
	}
	return best
}

// resolveCall finds the target symbol for a call site.
// Priority: same file → same directory → global name match.
func resolveCall(cs *parser.CallSite, byFile, byDir map[string][]*parser.Symbol, byName map[string][]*parser.Symbol) *parser.Symbol {
	name := cs.Name

	// 1. Same file
	if syms, ok := byFile[cs.File]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	// 2. Same directory (package)
	dir := filepath.Dir(cs.File)
	if syms, ok := byDir[dir]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	// 3. Global
	if syms, ok := byName[name]; ok && len(syms) > 0 {
		// If multiple matches, prefer the one closest to the caller's directory.
		callerDir := filepath.Dir(cs.File)
		return closestSymbol(syms, callerDir)
	}

	return nil
}

// findByName returns the first function/method with the given name from a symbol list.
func findByName(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

// closestSymbol returns the symbol whose file path shares the longest prefix with dir.
func closestSymbol(symbols []*parser.Symbol, dir string) *parser.Symbol {
	if len(symbols) == 0 {
		return nil
	}
	best := symbols[0]
	bestLen := commonPrefixLen(filepath.Dir(best.File), dir)
	for _, sym := range symbols[1:] {
		cl := commonPrefixLen(filepath.Dir(sym.File), dir)
		if cl > bestLen {
			bestLen = cl
			best = sym
		}
	}
	return best
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// --- Indexing helpers ---

func indexByName(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			m[sym.Name] = append(m[sym.Name], sym)
		}
	}
	return m
}

func indexByFile(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		m[sym.File] = append(m[sym.File], sym)
	}
	return m
}

func indexByDir(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		dir := filepath.Dir(sym.File)
		m[dir] = append(m[dir], sym)
	}
	return m
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestBuildCallGraph -v -count=1`
Expected: All 4 tests PASS.

**Step 5: Commit**

```bash
git add internal/callgraph/
git commit -m "feat(callgraph): add CallGraph types and BuildCallGraph with name-based resolution"
```

---

### Task 4: Call Chain Tracer (BFS with Depth Limit)

**Files:**
- Create: `internal/callgraph/trace.go` (Trace function)
- Create: `internal/callgraph/trace_test.go` (tests)

**Context:** Given a CallGraph and an entry point symbol name, trace the execution path using BFS up to a configurable depth (default 5). Detect and mark cycles. Support both "callees" (forward) and "callers" (reverse) directions.

**Step 1: Write the failing test**

Create `internal/callgraph/trace_test.go`:

```go
package callgraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestTrace_Callees(t *testing.T) {
	a := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/r/main.go", StartLine: 1, EndLine: 10}
	b := &parser.Symbol{Name: "serve", Kind: parser.KindFunction, File: "/r/server.go", StartLine: 1, EndLine: 20}
	c := &parser.Symbol{Name: "handle", Kind: parser.KindFunction, File: "/r/handler.go", StartLine: 1, EndLine: 15}

	g := &CallGraph{
		Symbols: []*parser.Symbol{a, b, c},
		Edges: []CallEdge{
			{Caller: a, Callee: b, CalleeName: "serve", Line: 5},
			{Caller: b, Callee: c, CalleeName: "handle", Line: 10},
		},
	}

	result := Trace(g, "main", TraceOpts{Direction: "callees", MaxDepth: 5})

	if result.Root == nil || result.Root.Name != "main" {
		t.Fatalf("root = %v, want main", result.Root)
	}
	if result.TotalNodes < 3 {
		t.Errorf("totalNodes = %d, want >= 3", result.TotalNodes)
	}
	if len(result.Tree) == 0 {
		t.Fatal("tree is empty")
	}
	// main -> serve -> handle
	if len(result.Tree[0].Children) == 0 {
		t.Fatal("main should have children (serve)")
	}
}

func TestTrace_Callers(t *testing.T) {
	a := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/r/main.go", StartLine: 1, EndLine: 10}
	b := &parser.Symbol{Name: "handle", Kind: parser.KindFunction, File: "/r/handler.go", StartLine: 1, EndLine: 15}

	g := &CallGraph{
		Symbols: []*parser.Symbol{a, b},
		Edges: []CallEdge{
			{Caller: a, Callee: b, CalleeName: "handle", Line: 5},
		},
	}

	result := Trace(g, "handle", TraceOpts{Direction: "callers", MaxDepth: 5})

	if result.Root == nil || result.Root.Name != "handle" {
		t.Fatalf("root = %v, want handle", result.Root)
	}
	if result.TotalNodes < 2 {
		t.Errorf("totalNodes = %d, want >= 2 (handle + main)", result.TotalNodes)
	}
}

func TestTrace_CycleDetection(t *testing.T) {
	a := &parser.Symbol{Name: "ping", Kind: parser.KindFunction, File: "/r/main.go", StartLine: 1, EndLine: 10}
	b := &parser.Symbol{Name: "pong", Kind: parser.KindFunction, File: "/r/main.go", StartLine: 11, EndLine: 20}

	g := &CallGraph{
		Symbols: []*parser.Symbol{a, b},
		Edges: []CallEdge{
			{Caller: a, Callee: b, CalleeName: "pong", Line: 5},
			{Caller: b, Callee: a, CalleeName: "ping", Line: 15},
		},
	}

	result := Trace(g, "ping", TraceOpts{Direction: "callees", MaxDepth: 10})

	// Should NOT infinite loop — cycle detected
	if result.TotalNodes > 10 {
		t.Errorf("totalNodes = %d, cycle detection failed", result.TotalNodes)
	}
}

func TestTrace_DepthLimit(t *testing.T) {
	// Chain: f0 -> f1 -> f2 -> f3 -> f4 -> f5
	symbols := make([]*parser.Symbol, 6)
	var edges []CallEdge
	for i := range 6 {
		symbols[i] = &parser.Symbol{
			Name: "f" + string(rune('0'+i)), Kind: parser.KindFunction,
			File: "/r/main.go", StartLine: uint32(i*10 + 1), EndLine: uint32(i*10 + 9),
		}
	}
	for i := range 5 {
		edges = append(edges, CallEdge{
			Caller: symbols[i], Callee: symbols[i+1],
			CalleeName: symbols[i+1].Name, Line: symbols[i].StartLine + 3,
		})
	}

	g := &CallGraph{Symbols: symbols, Edges: edges}

	// Depth 3: should only trace f0 -> f1 -> f2 -> f3
	result := Trace(g, "f0", TraceOpts{Direction: "callees", MaxDepth: 3})
	if result.MaxDepth > 3 {
		t.Errorf("maxDepth = %d, want <= 3", result.MaxDepth)
	}
}

func TestTrace_NotFound(t *testing.T) {
	g := &CallGraph{
		Symbols: []*parser.Symbol{
			{Name: "main", Kind: parser.KindFunction, File: "/r/main.go", StartLine: 1, EndLine: 10},
		},
	}

	result := Trace(g, "nonexistent", TraceOpts{Direction: "callees", MaxDepth: 5})
	if result.Root != nil {
		t.Errorf("root should be nil for nonexistent symbol")
	}
	if result.TotalNodes != 0 {
		t.Errorf("totalNodes = %d, want 0", result.TotalNodes)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestTrace -v -count=1`
Expected: FAIL — `Trace`, `TraceOpts` undefined.

**Step 3: Implement Trace**

Create `internal/callgraph/trace.go`:

```go
package callgraph

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

const defaultMaxDepth = 5

// TraceOpts controls how a call chain is traced.
type TraceOpts struct {
	// Direction is "callees" (forward: what does X call?) or "callers" (reverse: who calls X?).
	// Default: "callees".
	Direction string

	// MaxDepth limits how deep the trace goes. Default: 5. Max: 10.
	MaxDepth int
}

// CallChainNode is one node in a traced call tree.
type CallChainNode struct {
	// Symbol is the function at this node.
	Symbol *parser.Symbol `json:"symbol"`

	// Children are the next-level calls from this function.
	Children []CallChainNode `json:"children,omitempty"`

	// CallLine is the line in the parent where this call happens.
	CallLine uint32 `json:"callLine,omitempty"`

	// Cycle is true if this node was already visited (cycle detected).
	Cycle bool `json:"cycle,omitempty"`
}

// TraceResult is the output of a call chain trace.
type TraceResult struct {
	// Root is the entry-point symbol.
	Root *parser.Symbol `json:"root,omitempty"`

	// Tree is the call tree from the root.
	Tree []CallChainNode `json:"tree"`

	// MaxDepth is the deepest level actually reached.
	MaxDepth int `json:"maxDepth"`

	// TotalNodes is the total number of nodes in the tree.
	TotalNodes int `json:"totalNodes"`

	// Resolved is the count of edges pointing to known symbols.
	Resolved int `json:"resolved"`

	// Unresolved is the count of edges pointing to unknown symbols (stdlib, external).
	Unresolved int `json:"unresolved"`
}

// Trace traces a call chain from a named symbol through the call graph.
func Trace(g *CallGraph, symbolName string, opts TraceOpts) TraceResult {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if opts.MaxDepth > 10 {
		opts.MaxDepth = 10
	}
	if opts.Direction == "" {
		opts.Direction = "callees"
	}

	// Find the root symbol.
	root := findSymbol(g.Symbols, symbolName)
	if root == nil {
		return TraceResult{}
	}

	// Build adjacency index.
	var adjacency map[*parser.Symbol][]CallEdge
	if opts.Direction == "callers" {
		adjacency = buildCallerIndex(g.Edges)
	} else {
		adjacency = buildCalleeIndex(g.Edges)
	}

	visited := make(map[*parser.Symbol]bool)
	result := TraceResult{Root: root}

	node := traceNode(root, adjacency, visited, 0, opts.MaxDepth, &result)
	result.Tree = []CallChainNode{node}

	return result
}

// traceNode recursively builds the call tree from a given symbol.
func traceNode(sym *parser.Symbol, adj map[*parser.Symbol][]CallEdge, visited map[*parser.Symbol]bool, depth, maxDepth int, result *TraceResult) CallChainNode {
	result.TotalNodes++
	if depth > result.MaxDepth {
		result.MaxDepth = depth
	}

	node := CallChainNode{Symbol: sym}

	if depth >= maxDepth {
		return node
	}

	visited[sym] = true

	edges := adj[sym]
	for i := range edges {
		e := &edges[i]
		target := e.Callee
		if e.Caller == sym {
			// Forward direction: target is callee
			target = e.Callee
		} else {
			// Reverse direction: target is caller
			target = e.Caller
		}

		if target == nil {
			result.Unresolved++
			// Add unresolved as leaf
			node.Children = append(node.Children, CallChainNode{
				Symbol:   &parser.Symbol{Name: e.CalleeName, Kind: "external"},
				CallLine: e.Line,
			})
			continue
		}

		result.Resolved++

		if visited[target] {
			node.Children = append(node.Children, CallChainNode{
				Symbol:   target,
				CallLine: e.Line,
				Cycle:    true,
			})
			continue
		}

		child := traceNode(target, adj, visited, depth+1, maxDepth, result)
		child.CallLine = e.Line
		node.Children = append(node.Children, child)
	}

	visited[sym] = false // Allow visiting from different paths

	return node
}

// findSymbol finds the first function/method with the given name.
func findSymbol(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

// buildCalleeIndex maps each symbol to the edges where it is the caller.
func buildCalleeIndex(edges []CallEdge) map[*parser.Symbol][]CallEdge {
	m := make(map[*parser.Symbol][]CallEdge)
	for _, e := range edges {
		if e.Caller != nil {
			m[e.Caller] = append(m[e.Caller], e)
		}
	}
	return m
}

// buildCallerIndex maps each symbol to the edges where it is the callee.
func buildCallerIndex(edges []CallEdge) map[*parser.Symbol][]CallEdge {
	m := make(map[*parser.Symbol][]CallEdge)
	for _, e := range edges {
		if e.Callee != nil {
			m[e.Callee] = append(m[e.Callee], e)
		}
	}
	return m
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestTrace -v -count=1`
Expected: All 5 tests PASS.

**Step 5: Commit**

```bash
git add internal/callgraph/
git commit -m "feat(callgraph): add Trace with BFS, depth limit, cycle detection, and bidirectional support"
```

---

### Task 5: Orchestrator — TraceRepo (ingest + parse + extract + trace)

**Files:**
- Create: `internal/callgraph/repo.go` (TraceRepo: end-to-end orchestrator)
- Create: `internal/callgraph/repo_test.go` (integration test using go-code itself)

**Context:** `TraceRepo` ties everything together: ingest a repo, parse all files for symbols + calls in parallel, build the call graph, trace from the target symbol, and return the result. This is what the MCP tool handler will call.

**Step 1: Write the failing test**

Create `internal/callgraph/repo_test.go`:

```go
package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTraceRepo_Integration(t *testing.T) {
	// Create a tiny test repo with known call chains.
	dir := t.TempDir()

	mainGo := `package main

func main() {
	result := compute(42)
	println(result)
}

func compute(x int) int {
	return transform(x) + 1
}

func transform(x int) int {
	return x * 2
}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "main",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "main" {
		t.Fatalf("root = %v, want main", result.Root)
	}

	// main -> compute -> transform
	if result.TotalNodes < 3 {
		t.Errorf("totalNodes = %d, want >= 3 (main, compute, transform)", result.TotalNodes)
	}

	// println is unresolved (builtin)
	if result.Unresolved < 1 {
		t.Errorf("unresolved = %d, want >= 1 (println)", result.Unresolved)
	}
}

func TestTraceRepo_Callers(t *testing.T) {
	dir := t.TempDir()

	mainGo := `package main

func main() {
	serve()
}

func serve() {
	handle()
}

func handle() {
	println("done")
}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "handle",
		Opts:   TraceOpts{Direction: "callers", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "handle" {
		t.Fatalf("root = %v, want handle", result.Root)
	}

	// handle <- serve <- main
	if result.TotalNodes < 2 {
		t.Errorf("totalNodes = %d, want >= 2", result.TotalNodes)
	}
}

func TestTraceRepo_SymbolNotFound(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "nonexistent",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Root != nil {
		t.Errorf("root should be nil for nonexistent symbol")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestTraceRepo -v -count=1`
Expected: FAIL — `TraceRepo`, `TraceRepoInput` undefined.

**Step 3: Implement TraceRepo**

Create `internal/callgraph/repo.go`:

```go
package callgraph

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// maxFileBytes is the default maximum file size for parsing.
const maxFileBytes = 512 * 1024

// TraceRepoInput is the input for TraceRepo.
type TraceRepoInput struct {
	// Root is the local path to the repository.
	Root string

	// Symbol is the function/method name to trace from.
	Symbol string

	// Focus limits to a subdirectory.
	Focus string

	// Language limits to files of this language.
	Language string

	// Opts controls trace direction and depth.
	Opts TraceOpts
}

// parseResult pairs a file with its symbols and call sites.
type parseResult struct {
	symbols []*parser.Symbol
	calls   []parser.CallSite
}

// TraceRepo ingests a repository, extracts symbols and calls, builds a call graph,
// and traces the execution path from the named symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	results := parseFilesParallel(ctx, ir.Files)

	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
	}

	g := BuildCallGraph(allSymbols, allCalls)
	result := Trace(g, input.Symbol, input.Opts)

	return &result, nil
}

// parseFilesParallel parses all files for both symbols and call sites concurrently.
func parseFilesParallel(ctx context.Context, files []*ingest.File) []parseResult {
	results := make([]parseResult, len(files))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	work := make(chan int, len(files))
	for i := range files {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				results[idx] = parseFileForCalls(files[idx])
			}
		}()
	}

	wg.Wait()
	return results
}

// parseFileForCalls reads and parses a single file for symbols and call sites.
func parseFileForCalls(file *ingest.File) parseResult {
	source, err := os.ReadFile(file.Path)
	if err != nil {
		return parseResult{}
	}

	opts := parser.ParseOpts{
		Language:       file.Language,
		IncludeBody:    true,
		IncludeImports: true,
	}

	pr, err := parser.ParseFile(file.Path, source, opts)
	if err != nil {
		return parseResult{}
	}

	calls, _ := parser.ExtractCalls(file.Path, source, opts)

	return parseResult{
		symbols: pr.Symbols,
		calls:   calls,
	}
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/callgraph/ -run TestTraceRepo -v -count=1`
Expected: All 3 tests PASS.

Run full test suite:
Run: `cd /path/to/repos/src/go-code && go test ./... -count=1`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/callgraph/
git commit -m "feat(callgraph): add TraceRepo orchestrator with parallel parsing"
```

---

### Task 6: LLM Narrative + MCP Tool Handler

**Files:**
- Modify: `internal/llm/llm.go` (add SystemPromptCallTrace)
- Create: `cmd/go-code/tool_call_trace.go` (MCP tool handler)
- Modify: `cmd/go-code/register.go` (wire call_trace)
- Modify: `cmd/go-code/main.go` (bump toolCount to 6)

**Context:** The MCP tool handler accepts `{repo, symbol, depth, direction}`, calls `callgraph.TraceRepo()`, optionally gets an LLM narrative, and returns JSON. Follows the same patterns as `tool_code_compare.go`.

**Step 1: Add LLM system prompt**

Add to `internal/llm/llm.go` before `SystemPromptForDepth`:

```go
// SystemPromptCallTrace is the system prompt for call chain narrative generation.
const SystemPromptCallTrace = `You are a senior software engineer explaining an execution path through a codebase.
You receive a call chain trace (JSON tree of function calls).

Explain step-by-step what happens when the entry function is called:
1. What each function does (based on its name and signature)
2. Key decision points and error handling paths
3. External calls that leave the codebase (stdlib, third-party)
4. Cycles or recursive patterns if present

Be concise and focus on the flow, not line-by-line details.
Format as a numbered walkthrough.`
```

**Step 2: Create the MCP tool handler**

Create `cmd/go-code/tool_call_trace.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallTraceInput is the input schema for the call_trace tool.
type CallTraceInput struct {
	Repo      string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo) or absolute local path"`
	Symbol    string `json:"symbol" jsonschema_description:"Function or method name to trace (e.g. CompareRepos, Server.Serve)"`
	Depth     int    `json:"depth,omitempty" jsonschema_description:"Max trace depth (default 5, max 10)"`
	Direction string `json:"direction,omitempty" jsonschema_description:"Trace direction: callees (what does X call?) or callers (who calls X?). Default: callees"`
	Focus     string `json:"focus,omitempty" jsonschema_description:"Subdirectory to limit analysis to"`
	Language  string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
}

// callTraceOutput is the JSON output structure.
type callTraceOutput struct {
	Symbol    string                    `json:"symbol"`
	Direction string                    `json:"direction"`
	CallTree  []callgraph.CallChainNode `json:"call_tree"`
	Stats     traceStats                `json:"stats"`
	Narrative string                    `json:"narrative,omitempty"`
}

type traceStats struct {
	TotalNodes    int     `json:"total_nodes"`
	MaxDepth      int     `json:"max_depth"`
	Resolved      int     `json:"resolved"`
	Unresolved    int     `json:"unresolved"`
	ResolvedRatio float64 `json:"resolved_ratio"`
}

// registerCallTrace registers the call_trace MCP tool.
func registerCallTrace(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "call_trace",
		Description: "Trace the execution path of a function through a codebase. " +
			"Shows what happens when a function is called (callees) or who calls it (callers). " +
			"Returns a call tree with resolved cross-file references and an LLM-generated " +
			"narrative explanation of the execution flow.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallTraceInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}
		if input.Symbol == "" {
			return errResult("symbol is required"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}

		direction := input.Direction
		if direction == "" {
			direction = "callees"
		}

		result, err := callgraph.TraceRepo(ctx, callgraph.TraceRepoInput{
			Root:     root,
			Symbol:   input.Symbol,
			Focus:    input.Focus,
			Language: input.Language,
			Opts: callgraph.TraceOpts{
				Direction: direction,
				MaxDepth:  depth,
			},
		})
		if err != nil {
			return errResult(fmt.Sprintf("trace: %s", err)), nil, nil
		}

		if result.Root == nil {
			return errResult(fmt.Sprintf("symbol %q not found in repository", input.Symbol)), nil, nil
		}

		total := result.Resolved + result.Unresolved
		var ratio float64
		if total > 0 {
			ratio = float64(result.Resolved) / float64(total)
		}

		output := callTraceOutput{
			Symbol:    input.Symbol,
			Direction: direction,
			CallTree:  result.Tree,
			Stats: traceStats{
				TotalNodes:    result.TotalNodes,
				MaxDepth:      result.MaxDepth,
				Resolved:      result.Resolved,
				Unresolved:    result.Unresolved,
				ResolvedRatio: ratio,
			},
		}

		// LLM narrative (optional, non-fatal).
		if deps.LLM != nil && result.TotalNodes > 1 {
			treeJSON, _ := json.Marshal(result.Tree)
			prompt := fmt.Sprintf("Entry function: %s\nDirection: %s\n\nCall tree:\n%s",
				input.Symbol, direction, string(treeJSON))
			narrative, narErr := deps.LLM.Complete(ctx, llm.SystemPromptCallTrace, prompt)
			if narErr == nil {
				output.Narrative = narrative
			}
		}

		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}
```

**Step 3: Wire the tool in register.go**

Add to `registerTools()` in `cmd/go-code/register.go` after `registerSymbolSearch`:

```go
registerCallTrace(server, cfg, deps)
```

**Step 4: Bump toolCount in main.go**

Change `toolCount = 5` to `toolCount = 6` in `cmd/go-code/main.go:29`.

**Step 5: Build and verify**

Run: `cd /path/to/repos/src/go-code && go build ./...`
Expected: Clean build, no errors.

Run: `cd /path/to/repos/src/go-code && go test ./... -count=1`
Expected: All tests PASS.

**Step 6: Commit**

```bash
git add cmd/go-code/ internal/llm/
git commit -m "feat(mcp): add call_trace tool with LLM narrative"
```

---

### Task 7: Deploy + Update Docs + Verify

**Files:**
- Modify: `docs/ROADMAP.md` (mark Phase 4.1 complete)
- Modify: `CLAUDE.md` (add call_trace to tool table)

**Step 1: Rebuild and deploy Docker**

Run:
```bash
cd ~/deploy/example-server && docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```
Expected: Container starts, health check OK.

**Step 2: Verify health**

Run: `curl -s http://127.0.0.1:8897/health`
Expected: `{"status":"ok","service":"go-code","version":"..."}` with 6 tools.

**Step 3: Test call_trace via MCP — callees**

Run:
```bash
curl -s -X POST http://127.0.0.1:8897/mcp -H 'Content-Type: application/json' -d '{
  "jsonrpc": "2.0", "id": 1,
  "method": "tools/call",
  "params": {
    "name": "call_trace",
    "arguments": {
      "repo": "/host-src/go-code",
      "symbol": "CompareRepos",
      "depth": 3
    }
  }
}' | grep '^data:' | sed 's/^data: //' | jq '.result.content[0].text | fromjson | .stats'
```
Expected: JSON with `total_nodes > 0`, `resolved > 0`.

**Step 4: Test call_trace via MCP — callers**

Run:
```bash
curl -s -X POST http://127.0.0.1:8897/mcp -H 'Content-Type: application/json' -d '{
  "jsonrpc": "2.0", "id": 1,
  "method": "tools/call",
  "params": {
    "name": "call_trace",
    "arguments": {
      "repo": "/host-src/go-code",
      "symbol": "ParseFile",
      "direction": "callers",
      "depth": 2
    }
  }
}' | grep '^data:' | sed 's/^data: //' | jq '.result.content[0].text | fromjson | .stats'
```
Expected: JSON with callers of ParseFile resolved.

**Step 5: Update ROADMAP.md**

Add after Phase 3 section:

```markdown
### 4.1 Call chain tracing ✅
- [x] Call extraction via tree-sitter queries for all 9 languages
- [x] Name-based resolution: same-file → same-package → cross-package
- [x] BFS trace with configurable depth (default 5, max 10)
- [x] Bidirectional: callees (forward) and callers (reverse)
- [x] Cycle detection
- [x] LLM narrative explanation
- [x] `call_trace` MCP tool
```

**Step 6: Update CLAUDE.md tool table**

Add `call_trace` row to the MCP Tools table.

**Step 7: Commit**

```bash
git add docs/ROADMAP.md CLAUDE.md
git commit -m "docs: mark Phase 4.1 (call chain tracing) complete"
```

**Step 8: Tag release**

```bash
git tag v1.6.0
git push origin main --tags
```
