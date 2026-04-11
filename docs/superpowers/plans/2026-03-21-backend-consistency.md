# Backend Consistency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all go-code tools consistently use enhanced analysis backends (go/types, SCIP) and propagate tier/backend identity to users.

**Architecture:** Add `IsInterface` flag to `CallEdge`, propagate it from `TypedEdge` and SCIP. Use `IsInterface` edges in dead_code to eliminate method false positives. Add `Backend` field to `CallGraph` for identity. Add tier to tools that lack it (explore, dep_graph, code_health). Fix explore to use `BuildFromRepo`.

**Tech Stack:** Go 1.26, existing internal packages (callgraph, deadcode, tier, goanalysis, scip)

**All commands:** prefix with `GOWORK=off`. Working dir: `/home/krolik/src/go-code/`.

---

## File Structure

### Modified files

| File | Change |
|------|--------|
| `internal/callgraph/graph.go` | Add `IsInterface` to `CallEdge`, `Backend` to `CallGraph` |
| `internal/callgraph/convert.go` | Propagate `IsInterface` from `TypedEdge` |
| `internal/callgraph/scip.go` | Set `Backend` field |
| `internal/callgraph/repo.go` | Set `Backend` field for go/types path |
| `internal/deadcode/deadcode.go` | Use `IsInterface` edges to filter methods |
| `internal/explore/explore.go` | Switch from `BuildCallGraph` to `BuildFromRepo` |
| `cmd/go-code/tool_explore.go` | Add `tier` to output |

---

## Task 1: Add IsInterface to CallEdge + Backend to CallGraph

**Files:**
- Modify: `internal/callgraph/graph.go`
- Modify: `internal/callgraph/convert.go`
- Modify: `internal/callgraph/repo.go`
- Modify: `internal/callgraph/scip.go`

- [ ] **Step 1: Add fields to structs**

In `internal/callgraph/graph.go`:
```go
type CallEdge struct {
	Caller      *parser.Symbol
	Callee      *parser.Symbol
	CalleeName  string
	Receiver    string
	Line        uint32
	IsInterface bool   // true if this call goes through interface dispatch
}

type CallGraph struct {
	Edges         []CallEdge
	Symbols       []*parser.Symbol
	TypeRels      []parser.TypeRelationship
	HookCallbacks []string
	Tier          string // "basic", "enhanced", "full"
	Backend       string // "tree-sitter", "go/types", "scip", "tree-sitter+go/types", etc.
}
```

- [ ] **Step 2: Propagate IsInterface in convert.go**

In `ConvertToCallGraph`, set `IsInterface` from `TypedEdge`:
```go
edges = append(edges, CallEdge{
	Caller:      caller,
	Callee:      callee,
	CalleeName:  te.CalleeName,
	Receiver:    te.ReceiverType,
	Line:        te.Line,
	IsInterface: te.IsInterface,
})
```

- [ ] **Step 3: Set Backend in repo.go**

After `BuildCallGraph`: `cg.Backend = "tree-sitter"`
After go/types merge: `cg.Backend = "tree-sitter+go/types"`
After SCIP merge (in scip.go): `cg.Backend = "tree-sitter+scip"`

In `internal/callgraph/repo.go`, after line 60:
```go
cg.Backend = "tree-sitter"
```
After line 66 (go/types success):
```go
cg.Backend = "tree-sitter+go/types"
```

In `internal/callgraph/scip.go`, in the success path before return:
```go
slog.Info("scip: enhanced", ...)
cg := ConvertToCallGraph(typedEdges, tsSymbols)
cg.Backend = "tree-sitter+scip"  // Note: this is the typed subgraph, merged later
return cg
```
Actually — `MergeCallGraphs` returns a new graph. Set Backend on the result in repo.go:
```go
if scipCG := trySCIPResolution(...); scipCG != nil {
	cg = MergeCallGraphs(cg, scipCG)
	cg.Tier = "enhanced"
	cg.Backend = "tree-sitter+scip"
}
```

- [ ] **Step 4: Build and test**

Run: `cd /home/krolik/src/go-code && GOWORK=off go build ./... && GOWORK=off go test ./internal/callgraph/ -count=1 -timeout 120s`
Expected: All pass, no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/callgraph/
git commit -m "feat(callgraph): add IsInterface to CallEdge and Backend to CallGraph"
```

---

## Task 2: Use IsInterface edges in dead_code filtering

**Files:**
- Modify: `internal/deadcode/deadcode.go` (function `buildInterfaceInfo`)

- [ ] **Step 1: Write failing test**

In `internal/deadcode/deadcode_test.go`, add:
```go
func TestAnalyze_IsInterfaceEdgeExcludesMethod(t *testing.T) {
	// A method "Greet" on receiver "EnglishGreeter" has no direct callers,
	// but there's an IsInterface=true edge calling it through interface dispatch.
	// It should NOT appear in dead code.
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 5},
		{Name: "Greet", Kind: parser.KindMethod, Receiver: "EnglishGreeter", File: "greet.go", StartLine: 1, EndLine: 3},
	}
	edges := []callgraph.CallEdge{
		{
			Caller: symbols[0], CalleeName: "Greet", Receiver: "Greeter",
			Line: 3, IsInterface: true,
			// Note: Callee is nil — interface dispatch doesn't resolve to concrete impl
		},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: edges}
	result := deadcode.Analyze(cg, deadcode.Options{IncludeExported: true})
	for _, ds := range result.DeadSymbols {
		if ds.Name == "Greet" {
			t.Errorf("Greet should not be dead — it's called via interface dispatch")
		}
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `GOWORK=off go test ./internal/deadcode/ -v -run TestAnalyze_IsInterfaceEdge -count=1`
Expected: FAIL (Greet appears in dead code because edge has Callee=nil)

- [ ] **Step 3: Enhance buildInterfaceInfo to use IsInterface edges**

In `internal/deadcode/deadcode.go`, modify `buildInterfaceInfo`:
```go
func buildInterfaceInfo(symbols []*parser.Symbol, rels []parser.TypeRelationship, edges []callgraph.CallEdge) *interfaceInfo {
	info := &interfaceInfo{
		implementors:         make(map[string]bool),
		interfaceMethodNames: make(map[string]bool),
	}

	// Source 1: From call edges with IsInterface=true.
	// These are proven interface dispatches from go/types or SCIP.
	for _, e := range edges {
		if e.IsInterface && e.CalleeName != "" {
			info.interfaceMethodNames[e.CalleeName] = true
			if e.Receiver != "" {
				info.implementors[e.Receiver] = true
			}
		}
	}

	// Source 2: From tree-sitter type relationships.
	ifaceTypes := make(map[string]bool)
	for _, sym := range symbols {
		if sym.Kind == parser.KindInterface {
			ifaceTypes[sym.Name] = true
		}
	}
	for _, sym := range symbols {
		if sym.Kind == parser.KindMethod && ifaceTypes[sym.Receiver] {
			info.interfaceMethodNames[sym.Name] = true
		}
	}
	for _, rel := range rels {
		if rel.Kind == parser.RelImplements || rel.Kind == parser.RelExtends {
			info.implementors[rel.Subject] = true
		}
		if rel.Kind == parser.RelEmbeds && ifaceTypes[rel.Target] {
			info.implementors[rel.Subject] = true
		}
	}

	return info
}
```

Update the call in `Analyze`:
```go
ifaceInfo := buildInterfaceInfo(cg.Symbols, opts.Relationships, cg.Edges)
```

- [ ] **Step 4: Run test — verify it passes**

Run: `GOWORK=off go test ./internal/deadcode/ -v -run TestAnalyze_IsInterfaceEdge -count=1`
Expected: PASS

- [ ] **Step 5: Run full deadcode test suite**

Run: `GOWORK=off go test ./internal/deadcode/ -count=1 -timeout 30s`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/deadcode/deadcode.go internal/deadcode/deadcode_test.go
git commit -m "feat(deadcode): use IsInterface edges from go/types and SCIP for filtering"
```

---

## Task 3: Fix explore to use BuildFromRepo (adds go/types + SCIP)

**Files:**
- Modify: `internal/explore/explore.go`

- [ ] **Step 1: Replace BuildCallGraph with BuildFromRepo**

In `internal/explore/explore.go`, change line ~114:

From:
```go
cg := callgraph.BuildCallGraph(pr.symbols, pr.calls)
```

To:
```go
cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
	Root:     input.Root,
	Language: input.Language,
	Focus:    input.Focus,
})
if err != nil {
	// Fallback to basic callgraph on failure
	slog.Debug("explore: BuildFromRepo failed, falling back", "err", err)
	cg = callgraph.BuildCallGraph(pr.symbols, pr.calls)
}
```

Note: This requires `ctx context.Context` to be available. Check if `Run` already takes ctx — if not, add it.

- [ ] **Step 2: Add Tier to Result**

In `internal/explore/explore.go`, add to `Result` struct:
```go
Tier    string `json:"tier,omitempty"`
Backend string `json:"backend,omitempty"`
```

Set them:
```go
result.Tier = cg.Tier
result.Backend = cg.Backend
```

- [ ] **Step 3: Build and test**

Run: `GOWORK=off go build ./internal/explore/ && GOWORK=off go test ./internal/explore/ -count=1 -timeout 60s`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/explore/explore.go
git commit -m "feat(explore): use BuildFromRepo for enhanced tier + SCIP support"
```

---

## Task 4: Propagate tier to remaining tool outputs

**Files:**
- Modify: `cmd/go-code/tool_explore.go` (add tier to XML output)

- [ ] **Step 1: Check tool_explore.go output format**

Read `cmd/go-code/tool_explore.go` to understand how Result is serialized. Add `Tier` and `Backend` to JSON output.

- [ ] **Step 2: Add tier/backend fields to explore JSON output**

The explore tool likely returns JSON directly from `explore.Result`. Since we added `Tier` and `Backend` to Result, they'll be auto-included in JSON. Verify in the tool handler.

- [ ] **Step 3: Build and test**

Run: `GOWORK=off go build ./cmd/go-code/ && GOWORK=off go test ./cmd/go-code/ -count=1 -timeout 120s`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/go-code/
git commit -m "feat(explore): propagate tier and backend in tool output"
```

---

## Task 5: Add method-name matching from interface dispatch edges

The current `isInterfaceImpl` checks `receiver implements interface AND method name matches`. But with `IsInterface` edges we can do better: any method name appearing in an `IsInterface=true` edge is a proven interface method. Methods with the same name on other types implementing the same interface should also be excluded.

**Files:**
- Modify: `internal/deadcode/deadcode.go` (enhance `isInterfaceImpl`)

- [ ] **Step 1: Write test for cross-type interface filtering**

```go
func TestAnalyze_InterfaceMethodExcludesAllImplementors(t *testing.T) {
	// Interface "Greeter" has method "Greet".
	// EnglishGreeter and SpanishGreeter both implement it.
	// Only EnglishGreeter.Greet is called via interface dispatch.
	// SpanishGreeter.Greet should ALSO be excluded (same interface method).
	iface := &parser.Symbol{Name: "Greeter", Kind: parser.KindInterface, File: "a.go", StartLine: 1, EndLine: 3}
	eng := &parser.Symbol{Name: "Greet", Kind: parser.KindMethod, Receiver: "EnglishGreeter", File: "b.go", StartLine: 1, EndLine: 3}
	spa := &parser.Symbol{Name: "Greet", Kind: parser.KindMethod, Receiver: "SpanishGreeter", File: "c.go", StartLine: 1, EndLine: 3}
	main := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 5}
	symbols := []*parser.Symbol{iface, eng, spa, main}
	edges := []callgraph.CallEdge{
		{Caller: main, CalleeName: "Greet", Receiver: "Greeter", Line: 3, IsInterface: true},
	}
	rels := []parser.TypeRelationship{
		{Subject: "EnglishGreeter", Target: "Greeter", Kind: parser.RelImplements},
		{Subject: "SpanishGreeter", Target: "Greeter", Kind: parser.RelImplements},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: edges, TypeRels: rels}
	result := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: true,
		Relationships:   rels,
	})
	for _, ds := range result.DeadSymbols {
		if ds.Name == "Greet" {
			t.Errorf("Greet on %s should not be dead — interface method", ds.File)
		}
	}
}
```

- [ ] **Step 2: Run test — expect pass or fail depending on current logic**

If `isInterfaceImpl` already handles this via `interfaceMethodNames["Greet"]` + `implementors["SpanishGreeter"]`, it should pass. If not, fix.

- [ ] **Step 3: Commit if test passes**

```bash
git add internal/deadcode/
git commit -m "test(deadcode): verify cross-type interface method exclusion"
```

---

## Task 6: Full regression test + deploy

- [ ] **Step 1: Run full test suite**

Run: `cd /home/krolik/src/go-code && GOWORK=off go test ./... -timeout 300s -count=1`
Expected: All 30+ packages pass.

- [ ] **Step 2: Build Docker image**

Run: `cd ~/deploy/krolik-server && docker compose build go-code`
Expected: Build succeeds.

- [ ] **Step 3: Deploy**

Run: `cd ~/deploy/krolik-server && docker compose up -d --no-deps --force-recreate go-code`
Expected: Container healthy.

- [ ] **Step 4: Verify dead_code improvement**

Run `dead_code` on go-code itself with `include_exported=true`. Count should be significantly lower than 107 (target: <30).

- [ ] **Step 5: Verify explore shows tier**

Run `explore` on a Go repo — should show `"tier": "enhanced"`.

- [ ] **Step 6: Commit version bump if needed**

---

## Summary

| Task | What | Impact |
|------|------|--------|
| 1 | IsInterface on CallEdge + Backend identity | Foundation for all other tasks |
| 2 | dead_code uses IsInterface edges | Eliminates interface method false positives |
| 3 | explore uses BuildFromRepo | Gives explore go/types + SCIP |
| 4 | Tier in explore output | Users see analysis quality |
| 5 | Cross-type interface filtering test | Validates dead_code precision |
| 6 | Regression test + deploy | Ship it |

**Execution order:** 1 → 2 → 5 (can be parallel with 3-4) → 3 → 4 → 6
