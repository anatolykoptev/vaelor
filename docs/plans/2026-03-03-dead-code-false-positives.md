# Dead Code False Positives Fix — WordPress PHP + React JSX

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate 23/24 false positives when running `dead_code` on WordPress plugins with React/JSX components.

**Architecture:** Three root causes, three fix layers: (1) PHP hooks — `InjectHookEdges` only creates edges when both `add_action` and `do_action` exist in repo, but WP core hooks are invoked by WordPress core, not the plugin; fix by injecting edges for server-only hooks. (2) PHP constructors — `__construct` called implicitly via `new ClassName()`, but `php_calls.scm` lacks `object_creation_expression` pattern; fix both query and deadcode filter. (3) JSX — TypeScript grammar can't parse JSX; `.tsx`/`.jsx` need TSX grammar with JSX call patterns.

**Tech Stack:** Go, tree-sitter (C bindings), tree-sitter queries (.scm), `github.com/smacker/go-tree-sitter/typescript/tsx`

---

### Task 1: Fix InjectHookEdges for WordPress Core Hooks

Server-side hook registrations (`add_action('admin_notices', [$this, 'method'])`) prove the callback is alive — WordPress core WILL call it. Currently edges are only created when the repo also contains `do_action('admin_notices')`, which never happens for WP core hooks.

**Files:**
- Modify: `internal/callgraph/graph.go:135-178`
- Test: `internal/deadcode/deadcode_test.go`

**Step 1: Write the failing test**

Add to `internal/deadcode/deadcode_test.go`:

```go
// TestAnalyze_ServerOnlyHooks verifies that WordPress hooks registered with
// add_action/add_filter (server-side only, no do_action in repo) are NOT
// flagged as dead. This covers WP core hooks like admin_notices, init, etc.
func TestAnalyze_ServerOnlyHooks(t *testing.T) {
	enqueueEditor := &parser.Symbol{
		Name: "enqueue_editor", Kind: parser.KindMethod,
		File: "/plugin/assets.php", StartLine: 10, EndLine: 30,
	}
	renderNotice := &parser.Symbol{
		Name: "render_admin_notice", Kind: parser.KindMethod,
		File: "/plugin/license.php", StartLine: 20, EndLine: 40,
	}
	genuinelyDead := &parser.Symbol{
		Name: "old_unused", Kind: parser.KindFunction,
		File: "/plugin/legacy.php", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/plugin/main.php", StartLine: 1, EndLine: 8,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{enqueueEditor, renderNotice, genuinelyDead, mainSym},
		Edges:   nil,
	}

	// Inject ONLY server-side hooks (no client-side do_action in repo).
	hookRoutes := []callgraph.HookRoute{
		{Method: "ACTION", Path: "enqueue_block_editor_assets", Handler: "enqueue_editor", Side: "server"},
		{Method: "ACTION", Path: "admin_notices", Handler: "render_admin_notice", Side: "server"},
		// No client-side routes — these are WordPress core hooks.
	}
	callgraph.InjectHookEdges(cg, hookRoutes)

	result := Analyze(cg, Options{})

	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead (old_unused), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
	if result.DeadCount == 1 && result.DeadSymbols[0].Name != "old_unused" {
		t.Errorf("expected dead 'old_unused', got %q", result.DeadSymbols[0].Name)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-code && go test ./internal/deadcode/... -v -run TestAnalyze_ServerOnlyHooks -count=1`
Expected: FAIL — enqueue_editor and render_admin_notice reported as dead (no edges injected for server-only hooks)

**Step 3: Fix InjectHookEdges**

In `internal/callgraph/graph.go`, modify `InjectHookEdges` to also inject edges for server-side hooks that have NO corresponding client-side invocation. After the existing client→server loop, add:

```go
// For server-side hooks with no client-side invocation (e.g. WordPress core
// hooks like admin_notices, init, enqueue_block_editor_assets), inject edges
// directly. The add_action/add_filter registration itself proves the callback
// is alive — WordPress core will invoke it.
for _, r := range hookRoutes {
	if r.Side != "server" || r.Handler == "" {
		continue
	}
	targets := byName[r.Handler]
	for _, target := range targets {
		if !called[target] {
			cg.Edges = append(cg.Edges, CallEdge{
				Callee:     target,
				CalleeName: r.Handler,
				Line:       r.Line,
			})
			called[target] = true
		}
	}
	if len(targets) == 0 {
		cg.Edges = append(cg.Edges, CallEdge{
			CalleeName: r.Handler,
			Line:       r.Line,
		})
	}
}
```

Note: need to build a `called` set before this loop to avoid duplicating edges already created by the client→server pass. Refactor the function to track which targets already have edges.

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-code && go test ./internal/deadcode/... -v -run TestAnalyze -count=1`
Expected: ALL deadcode tests PASS

**Step 5: Commit**

```bash
cd ~/src/go-code
git add internal/callgraph/graph.go internal/deadcode/deadcode_test.go
git commit -m "fix: inject hook edges for server-only WP hooks (core hooks)"
```

---

### Task 2: Skip Constructors in Dead Code Analysis

`__construct` (PHP), `__init__` (Python), `constructor` (JS/TS class) are called implicitly by `new ClassName()` and can never be truly dead.

**Files:**
- Modify: `internal/deadcode/deadcode.go:138-153`
- Test: `internal/deadcode/deadcode_test.go`

**Step 1: Write the failing test**

```go
// TestAnalyze_ConstructorNotDead verifies that language constructors are
// not flagged as dead code — they're called implicitly by new ClassName().
func TestAnalyze_ConstructorNotDead(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"__construct", "/app/class.php"},
		{"__init__", "/app/class.py"},
		{"constructor", "/app/class.ts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sym := &parser.Symbol{
				Name: tc.name, Kind: parser.KindMethod,
				File: tc.file, StartLine: 5, EndLine: 15,
			}
			mainSym := &parser.Symbol{
				Name: "main", Kind: parser.KindFunction,
				File: tc.file, StartLine: 1, EndLine: 3,
			}
			cg := &callgraph.CallGraph{
				Symbols: []*parser.Symbol{sym, mainSym},
				Edges:   nil,
			}
			result := Analyze(cg, Options{})
			for _, d := range result.DeadSymbols {
				if d.Name == tc.name {
					t.Errorf("%s should not be flagged as dead (implicit constructor)", tc.name)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-code && go test ./internal/deadcode/... -v -run TestAnalyze_ConstructorNotDead -count=1`
Expected: FAIL — `__construct`, `__init__`, `constructor` reported as dead

**Step 3: Add constructor skip to shouldSkipSymbol**

In `internal/deadcode/deadcode.go`, add a constructor check set and check it in `shouldSkipSymbol`:

```go
// constructorNames are implicitly-called constructors across languages.
var constructorNames = map[string]bool{
	"__construct": true, // PHP
	"__init__":    true, // Python
	"constructor": true, // JS/TS class
}
```

Add to `shouldSkipSymbol` after the entry point check:

```go
if constructorNames[sym.Name] {
	return true
}
```

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-code && go test ./internal/deadcode/... -v -run TestAnalyze -count=1`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/deadcode/deadcode.go internal/deadcode/deadcode_test.go
git commit -m "fix: skip constructors (__construct, __init__, constructor) in dead code"
```

---

### Task 3: Add PHP `new ClassName()` to Calls Query

`new Settings()` instantiates a class but `php_calls.scm` doesn't capture `object_creation_expression`. This means class constructors have no incoming call edges.

**Files:**
- Modify: `internal/parser/queries/php_calls.scm`
- Test: `internal/parser/calls_test.go`

**Step 1: Write the failing test**

Add to `internal/parser/calls_test.go`:

```go
func TestExtractCalls_PHPNewExpression(t *testing.T) {
	source := []byte(`<?php
class Settings {
    public function __construct() {}
    public function register() {}
}

class Plugin {
    public function init() {
        $settings = new Settings();
        $settings->register();
        $license = new \GigienaTeksta\License();
    }
}
`)
	calls, err := ExtractCalls("plugin.php", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	// "register" — method call (already works)
	if !found["register"] {
		t.Error("missing call to 'register'")
	}

	// "Settings" — new expression should be captured as a call
	if !found["Settings"] {
		t.Error("missing call to 'Settings' from new expression")
	}

	// "License" — qualified new expression
	if !found["License"] {
		t.Error("missing call to 'License' from qualified new expression")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-code && go test ./internal/parser/... -v -run TestExtractCalls_PHPNewExpression -count=1`
Expected: FAIL — "Settings" and "License" not found in calls

**Step 3: Add object_creation_expression to php_calls.scm**

Append to `internal/parser/queries/php_calls.scm`:

```scheme
; Constructor calls: new ClassName()
(object_creation_expression
  (qualified_name (name) @call.function))

; Constructor calls: new ClassName() (simple name)
(object_creation_expression
  (name) @call.function)
```

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-code && go test ./internal/parser/... -v -run TestExtractCalls_PHP -count=1`
Expected: ALL PHP call tests PASS

**Step 5: Commit**

```bash
git add internal/parser/queries/php_calls.scm internal/parser/calls_test.go
git commit -m "feat: detect new ClassName() as call in PHP dead code analysis"
```

---

### Task 4: Add TSX Grammar for JSX Support

The TypeScript grammar can't parse JSX. Files like `.tsx`/`.jsx` are parsed error-tolerantly — JSX attribute references like `onClick={handler}` are invisible. Need TSX grammar + handler.

**Files:**
- Create: `internal/parser/handler_tsx.go`
- Create: `internal/parser/queries/tsx_calls.scm`
- Modify: `internal/parser/handler_typescript.go:53-54` (remove .tsx/.jsx from extensions)
- Modify: `internal/parser/parser.go` (no changes needed, extToLanguage already maps .tsx/.jsx to correct language)
- Test: `internal/parser/calls_test.go`
- Vendor: `go mod vendor` after importing `typescript/tsx`

**Step 1: Write the failing test**

Add to `internal/parser/calls_test.go`:

```go
func TestExtractCalls_JSXAttributeRef(t *testing.T) {
	source := []byte(`
import { useState } from 'react';

const handleReplace = (word) => {
    console.log(word);
};

const handleCheck = () => {
    checkText();
};

const Component = () => {
    return (
        <div>
            <Button onClick={handleCheck} />
            <Button onClick={() => handleReplace('word')} />
        </div>
    );
};

export default Component;
`)
	calls, err := ExtractCalls("component.tsx", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	// handleCheck — JSX attribute reference (not a call expression)
	if !found["handleCheck"] {
		t.Error("missing JSX reference to 'handleCheck'")
	}

	// handleReplace — called inside arrow function in JSX attribute
	// This is a regular call_expression, should work with existing query
	if !found["handleReplace"] {
		t.Error("missing call to 'handleReplace'")
	}

	// checkText — regular call inside handleCheck function
	if !found["checkText"] {
		t.Error("missing call to 'checkText'")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-code && go test ./internal/parser/... -v -run TestExtractCalls_JSXAttributeRef -count=1`
Expected: FAIL — handleCheck not found (JSX attribute reference invisible to TS grammar)

**Step 3: Vendor the TSX grammar**

```bash
cd ~/src/go-code
# The tsx package is already available in the module, just not vendored
go mod vendor
```

**Step 4: Create tsx_calls.scm**

Create `internal/parser/queries/tsx_calls.scm` — same as `typescript_calls.scm` plus JSX patterns:

```scheme
; Regular function calls
(call_expression
  function: (identifier) @call.function)

; Method calls
(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))

; Function references passed as arguments
(call_expression
  arguments: (arguments
    (identifier) @call.function))

; JSX expression references: onClick={handler}, ref={myRef}
; Captures function identifiers used as JSX attribute values.
(jsx_expression
  (identifier) @call.function)
```

**Step 5: Create handler_tsx.go**

Create `internal/parser/handler_tsx.go`:

```go
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
)

//go:embed queries/tsx_calls.scm
var tsxCallsQueryBytes []byte

// tsxHandler handles .tsx and .jsx files using the TSX grammar (TypeScript + JSX).
// Reuses the TypeScript tags/rels queries (all TS node types exist in TSX grammar)
// but uses a separate calls query with JSX-specific patterns.
type tsxHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
	relsQuery *sitter.Query
}

var tsxLang = &tsxHandler{}

func init() {
	lang := tsx.GetLanguage()
	q, err := sitter.NewQuery(typescriptQueryBytes, lang)
	if err != nil {
		panic("typescript.scm (tsx) query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(tsxCallsQueryBytes, lang)
	if err != nil {
		panic("tsx_calls.scm query compile error: " + err.Error())
	}
	rq, err := sitter.NewQuery(tsRelsQueryBytes, lang)
	if err != nil {
		panic("typescript_rels.scm (tsx) query compile error: " + err.Error())
	}
	tsxLang.lang = lang
	tsxLang.query = q
	tsxLang.callQuery = cq
	tsxLang.relsQuery = rq
	registerHandler(tsxLang)
}

func (h *tsxHandler) Language() string           { return "typescript" }
func (h *tsxHandler) Extensions() []string       { return []string{".tsx", ".jsx"} }
func (h *tsxHandler) SitterLanguage() *sitter.Language { return h.lang }
func (h *tsxHandler) TagsQuery() *sitter.Query         { return h.query }
func (h *tsxHandler) CallsQuery() *sitter.Query        { return h.callQuery }
func (h *tsxHandler) RelationshipsQuery() *sitter.Query { return h.relsQuery }

// MapCapture delegates to the shared TypeScript capture mapper.
// TSX shares all symbol types with TypeScript (function, class, method, etc.)
func (h *tsxHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}
```

**Step 6: Remove .tsx/.jsx from TypeScript handler**

In `internal/parser/handler_typescript.go:53-54`, change:

```go
func (h *typescriptHandler) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
}
```

to:

```go
func (h *typescriptHandler) Extensions() []string {
	return []string{".ts", ".js", ".mjs", ".cjs"}
}
```

**Step 7: Run test to verify it passes**

Run: `cd ~/src/go-code && go test ./internal/parser/... -v -run TestExtractCalls -count=1`
Expected: ALL call extraction tests PASS (Go, Python, PHP, JSX)

**Step 8: Commit**

```bash
git add internal/parser/handler_tsx.go internal/parser/queries/tsx_calls.scm \
  internal/parser/handler_typescript.go internal/parser/calls_test.go vendor/
git commit -m "feat: add TSX grammar for JSX attribute reference detection"
```

---

### Task 5: Wire Hook Callbacks into dead_code Tool Handler

`handleDeadCode` in `tool_dead_code.go` never passes `HookCallbacks` to `deadcode.Options`. The `BuildFromRepo` call graph already has hook edges injected (Task 1 fixes server-only), but as a safety net, also pass the callback names explicitly.

**Files:**
- Modify: `internal/callgraph/graph.go` (add `HookCallbackNames` method)
- Modify: `cmd/go-code/tool_dead_code.go:62-84`

**Step 1: Add HookCallbackNames to CallGraph**

Add to `internal/callgraph/graph.go`:

```go
// HookCallbackNames returns the set of function names registered as hook
// callbacks. Used by dead code analysis as a safety net — even if edge
// injection somehow misses a callback, it won't be flagged as dead.
func (cg *CallGraph) HookCallbackNames() []string {
	var names []string
	seen := make(map[string]bool)
	for _, e := range cg.Edges {
		if e.Caller == nil && e.CalleeName != "" && !seen[e.CalleeName] {
			// Edges with nil Caller are synthetic hook edges.
			seen[e.CalleeName] = true
			names = append(names, e.CalleeName)
		}
	}
	return names
}
```

Actually, this approach is fragile. Better: store callback names during `InjectHookEdges` on the `CallGraph`. Add a field:

```go
type CallGraph struct {
	Edges          []CallEdge
	Symbols        []*parser.Symbol
	HookCallbacks  []string // function names registered as hook callbacks
}
```

Populate it in `InjectHookEdges`:

```go
// Collect all callback names for dead code safety net.
var cbNames []string
for _, r := range hookRoutes {
	if r.Side == "server" && r.Handler != "" {
		cbNames = append(cbNames, r.Handler)
	}
}
cg.HookCallbacks = cbNames
```

**Step 2: Wire it in tool_dead_code.go**

Change `handleDeadCode` from:

```go
result := deadcode.Analyze(cg, deadcode.Options{
	IncludeExported: input.IncludeExported,
})
```

to:

```go
result := deadcode.Analyze(cg, deadcode.Options{
	IncludeExported: input.IncludeExported,
	HookCallbacks:   cg.HookCallbacks,
})
```

**Step 3: Run all tests**

Run: `cd ~/src/go-code && go test ./... -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/callgraph/graph.go cmd/go-code/tool_dead_code.go
git commit -m "fix: wire hook callback names into dead_code options"
```

---

### Task 6: Build, Deploy, Verify

**Step 1: Build**

```bash
cd ~/src/go-code && go build ./...
```

**Step 2: Deploy**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
curl http://127.0.0.1:8897/health
```

**Step 3: Verify with gigiena-teksta**

Run `dead_code` on the plugin that triggered this fix. Expected: 0-1 dead symbols (only genuinely dead code), NOT 24.

**Step 4: Commit any remaining fixes**

If verification reveals issues, fix and commit.
