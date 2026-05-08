# v1.18: Type-Aware Analysis — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add compiler-level type analysis for Go repos via `go/types` + optional VTA call graphs, compound tools (`understand`, `prepare_change`), and 3-tier degradation system.

**Architecture:** New `internal/goanalysis/` package loads Go packages via `go/packages` and resolves calls using `go/types.Info`. Optional VTA mode builds SSA for precise interface dispatch. New `internal/tier/` package gates tool capabilities by available backends. Compound tools in `internal/compound/` aggregate sub-queries in parallel with partial failure tolerance.

**Tech Stack:** `golang.org/x/tools/go/packages`, `go/types`, `golang.org/x/tools/go/callgraph/vta`, `golang.org/x/tools/go/ssa/ssautil`

**Research refs:** CKB/CodeMCP (3-tier, compound tools), golang.org/x/tools/go/callgraph (VTA>RTA for libraries), gopls (go/types without SSA).

---

## Chunk 1: Tier System + Go Type Loader

### Task 1: Tier Detection System

**Files:**
- Create: `internal/tier/tier.go`
- Create: `internal/tier/tier_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/tier/tier_test.go
package tier

import "testing"

func TestDetectBasic(t *testing.T) {
	d := NewDetector(Backends{})
	if d.Current() != Basic {
		t.Fatalf("expected Basic, got %s", d.Current())
	}
}

func TestDetectEnhanced(t *testing.T) {
	d := NewDetector(Backends{GoTypes: true})
	if d.Current() != Enhanced {
		t.Fatalf("expected Enhanced, got %s", d.Current())
	}
}

func TestDetectFull(t *testing.T) {
	d := NewDetector(Backends{GoTypes: true, VTA: true})
	if d.Current() != Full {
		t.Fatalf("expected Full, got %s", d.Current())
	}
}

func TestDegradationWarnings(t *testing.T) {
	d := NewDetector(Backends{})
	warnings := d.Warnings()
	if len(warnings) == 0 {
		t.Fatal("expected warnings for Basic tier")
	}
	if warnings[0].CapabilityPct != 40 {
		t.Fatalf("expected 40%% capability, got %d", warnings[0].CapabilityPct)
	}
}

func TestTierString(t *testing.T) {
	tests := []struct {
		tier Tier
		want string
	}{
		{Basic, "basic"},
		{Enhanced, "enhanced"},
		{Full, "full"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("Tier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/tier/ -v -count=1`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

```go
// internal/tier/tier.go
package tier

// Tier represents the analysis capability level.
type Tier int

const (
	Basic    Tier = 1 // tree-sitter only
	Enhanced Tier = 2 // tree-sitter + go/types
	Full     Tier = 3 // tree-sitter + go/types + VTA call graph
)

func (t Tier) String() string {
	switch t {
	case Enhanced:
		return "enhanced"
	case Full:
		return "full"
	default:
		return "basic"
	}
}

// Backends tracks which analysis backends are available.
type Backends struct {
	GoTypes bool // go/packages loaded successfully
	VTA     bool // VTA call graph built successfully
	Graph   bool // Apache AGE graph available
	LLM     bool // LLM available for narratives
}

// DegradationWarning tells the AI what's missing and how to fix it.
type DegradationWarning struct {
	Code          string `json:"code" xml:"code,attr"`
	Message       string `json:"message" xml:"message,attr"`
	CapabilityPct int    `json:"capability_pct" xml:"capabilityPct,attr"`
}

// Provenance tracks which backends contributed to a result.
type Provenance struct {
	Tier     string   `json:"tier" xml:"tier,attr"`
	Backends []string `json:"backends" xml:"backend"`
}

// Detector resolves the effective tier from available backends.
type Detector struct {
	backends Backends
}

// NewDetector creates a tier detector from available backends.
func NewDetector(b Backends) *Detector {
	return &Detector{backends: b}
}

// Current returns the highest available tier.
func (d *Detector) Current() Tier {
	if d.backends.GoTypes && d.backends.VTA {
		return Full
	}
	if d.backends.GoTypes {
		return Enhanced
	}
	return Basic
}

// Warnings returns degradation warnings for the current tier.
func (d *Detector) Warnings() []DegradationWarning {
	var w []DegradationWarning
	if !d.backends.GoTypes {
		w = append(w, DegradationWarning{
			Code:          "go_types_missing",
			Message:       "Go type analysis unavailable — using name-based resolution (less precise for interfaces). Ensure repo has go.mod and is buildable.",
			CapabilityPct: 40,
		})
	} else if !d.backends.VTA {
		w = append(w, DegradationWarning{
			Code:          "vta_missing",
			Message:       "VTA call graph unavailable — using go/types resolution (precise for direct calls, approximate for interfaces).",
			CapabilityPct: 70,
		})
	}
	return w
}

// ProvenanceFor builds provenance metadata for a result.
func (d *Detector) ProvenanceFor(used ...string) Provenance {
	return Provenance{
		Tier:     d.Current().String(),
		Backends: used,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/tier/ -v -count=1`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
cd $REPO_ROOT && git add internal/tier/ && git commit -m "feat: add tier detection system for 3-level analysis capabilities"
```

---

### Task 2: Go Package Loader

**Files:**
- Create: `internal/goanalysis/loader.go`
- Create: `internal/goanalysis/loader_test.go`

**Prereqs:** None (independent of Task 1)

- [ ] **Step 1: Write the failing test**

```go
// internal/goanalysis/loader_test.go
package goanalysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPackages_ValidRepo(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := LoadPackages(ctx, dir, LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected at least one package")
	}
	if result.Packages[0].TypesInfo == nil {
		t.Fatal("expected TypesInfo to be populated")
	}
}

func TestLoadPackages_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func main() {}`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := LoadPackages(ctx, dir, LoadOpts{})
	if err == nil {
		t.Fatal("expected error for repo without go.mod")
	}
}

func TestLoadPackages_Timeout(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	_, err := LoadPackages(ctx, dir, LoadOpts{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestLoadPackages_Focus(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := LoadPackages(ctx, dir, LoadOpts{Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected packages")
	}
}

// makeTestModule creates a minimal Go module in a temp dir.
func makeTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Greeter interface {
	Greet() string
}

type Hello struct{ Name string }

func (h Hello) Greet() string { return "hello " + h.Name }

func greet(g Greeter) { fmt.Println(g.Greet()) }

func main() { greet(Hello{Name: "world"}) }
`)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -v -count=1 -timeout 60s`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

```go
// internal/goanalysis/loader.go
package goanalysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"
)

// defaultTimeout is the maximum time for package loading.
const defaultTimeout = 60 * time.Second

// LoadOpts configures package loading.
type LoadOpts struct {
	// Patterns are the package patterns to load (default: "./...").
	Patterns []string

	// Timeout overrides the default loading timeout.
	Timeout time.Duration
}

// LoadResult contains the loaded packages with full type information.
type LoadResult struct {
	Packages []*packages.Package
	Errors   []string // non-fatal errors (packages that failed to type-check)
}

// LoadPackages loads Go packages from a directory with full type information.
// Returns an error if go.mod is missing or the context expires.
func LoadPackages(ctx context.Context, dir string, opts LoadOpts) (*LoadResult, error) {
	// Verify go.mod exists.
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return nil, fmt.Errorf("not a Go module (no go.mod): %w", err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
		Dir:     dir,
		Context: ctx,
		Env:     append(os.Environ(), "GOFLAGS=-mod=mod"),
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	result := &LoadResult{}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			for _, e := range pkg.Errors {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", pkg.PkgPath, e.Msg))
			}
			// Still include packages with errors — they may have partial type info.
		}
		if pkg.TypesInfo != nil {
			result.Packages = append(result.Packages, pkg)
		}
	}

	if len(result.Packages) == 0 {
		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("no packages loaded: %s", result.Errors[0])
		}
		return nil, fmt.Errorf("no Go packages found in %s", dir)
	}

	return result, nil
}

// HasGoModule checks if a directory contains a go.mod file.
func HasGoModule(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -v -count=1 -timeout 60s`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
cd $REPO_ROOT && git add internal/goanalysis/ && git commit -m "feat: add Go package loader with go/types support"
```

---

### Task 3: Type-Aware Call Resolver

**Files:**
- Create: `internal/goanalysis/resolver.go`
- Create: `internal/goanalysis/resolver_test.go`

**Prereqs:** Task 2 (uses LoadResult)

- [ ] **Step 1: Write the failing test**

```go
// internal/goanalysis/resolver_test.go
package goanalysis

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestResolve_DirectCalls(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := LoadPackages(ctx, dir, LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}

	edges := Resolve(lr.Packages)
	if len(edges) == 0 {
		t.Fatal("expected call edges")
	}

	// main -> greet should be resolved.
	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected main -> greet edge")
	}
}

func TestResolve_InterfaceCalls(t *testing.T) {
	dir := makeInterfaceModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := LoadPackages(ctx, dir, LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}

	edges := Resolve(lr.Packages)

	// greet calls g.Greet() on interface — should resolve to Hello.Greet and World.Greet.
	var targets []string
	for _, e := range edges {
		if e.CallerName == "greet" && e.CalleeName == "Greet" {
			targets = append(targets, e.ReceiverType)
		}
	}
	if len(targets) < 2 {
		t.Errorf("expected >=2 interface dispatch targets, got %d: %v", len(targets), targets)
	}
}

func TestResolve_MethodCalls(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := LoadPackages(ctx, dir, LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}

	edges := Resolve(lr.Packages)
	// greet -> Greeter.Greet should be resolved.
	found := false
	for _, e := range edges {
		if e.CallerName == "greet" && e.CalleeName == "Greet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected greet -> Greet edge")
	}
}

func makeInterfaceModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/iface\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Greeter interface { Greet() string }

type Hello struct{}
func (Hello) Greet() string { return "hello" }

type World struct{}
func (World) Greet() string { return "world" }

func greet(g Greeter) { fmt.Println(g.Greet()) }

func main() {
	greet(Hello{})
	greet(World{})
}
`)
	return dir
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -run TestResolve -v -count=1 -timeout 60s`
Expected: FAIL — Resolve not defined

- [ ] **Step 3: Write implementation**

```go
// internal/goanalysis/resolver.go
package goanalysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

// TypedEdge is a call edge with full type information from the Go compiler.
type TypedEdge struct {
	CallerName   string // function containing the call
	CallerFile   string // absolute path
	CallerLine   uint32 // line of the caller function definition
	CalleeName   string // called function/method name
	CalleeFile   string // absolute path (empty if external)
	CalleePkg    string // package path of callee
	ReceiverType string // receiver type for method calls (empty for functions)
	Line         uint32 // line of the call site
	IsInterface  bool   // true if this is interface dispatch
}

// Resolve walks loaded packages and extracts type-aware call edges.
// For interface method calls, it resolves to all concrete implementations
// found in the loaded packages.
func Resolve(pkgs []*packages.Package) []TypedEdge {
	// Collect all concrete types across packages for interface resolution.
	allTypes := collectConcreteTypes(pkgs)

	var edges []TypedEdge
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			fpath := pkg.Fset.Position(file.Pos()).Filename
			edges = append(edges, resolveFile(pkg, file, fpath, allTypes)...)
		}
	}
	return edges
}

// resolveFile extracts call edges from a single AST file.
func resolveFile(pkg *packages.Package, file *ast.File, fpath string, allTypes []types.Type) []TypedEdge {
	var edges []TypedEdge

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		callerFunc := enclosingFunc(pkg.Fset, file, call.Pos())
		if callerFunc == "" {
			return true
		}

		callLine := uint32(pkg.Fset.Position(call.Pos()).Line)

		switch fn := call.Fun.(type) {
		case *ast.Ident:
			// Direct function call: foo()
			if obj, ok := pkg.TypesInfo.Uses[fn]; ok {
				edge := TypedEdge{
					CallerName: callerFunc,
					CallerFile: fpath,
					CalleeName: fn.Name,
					Line:       callLine,
				}
				if f, ok := obj.(*types.Func); ok {
					edge.CalleePkg = f.Pkg().Path()
					edge.CalleeFile = posFile(pkg.Fset, f.Pos())
				}
				edges = append(edges, edge)
			}

		case *ast.SelectorExpr:
			// Method or qualified call: x.Method() or pkg.Func()
			sel, ok := pkg.TypesInfo.Selections[fn]
			if ok {
				// Method call on a value.
				edge := makeMethodEdge(pkg.Fset, fpath, callerFunc, fn, sel, callLine, allTypes)
				edges = append(edges, edge...)
			} else if obj, ok := pkg.TypesInfo.Uses[fn.Sel]; ok {
				// Qualified function call: pkg.Func()
				edge := TypedEdge{
					CallerName: callerFunc,
					CallerFile: fpath,
					CalleeName: fn.Sel.Name,
					Line:       callLine,
				}
				if f, ok := obj.(*types.Func); ok {
					edge.CalleePkg = f.Pkg().Path()
					edge.CalleeFile = posFile(pkg.Fset, f.Pos())
				}
				edges = append(edges, edge)
			}
		}
		return true
	})

	return edges
}

// makeMethodEdge creates edges for a method call, resolving interface dispatch.
func makeMethodEdge(fset *token.FileSet, fpath, caller string, sel *ast.SelectorExpr, selection *types.Selection, line uint32, allTypes []types.Type) []TypedEdge {
	recvType := selection.Recv()
	methodName := sel.Sel.Name

	base := TypedEdge{
		CallerName: caller,
		CallerFile: fpath,
		CalleeName: methodName,
		Line:       line,
	}

	// Check if receiver is an interface.
	iface, isIface := recvType.Underlying().(*types.Interface)
	if !isIface {
		// Concrete method call — single edge.
		base.ReceiverType = typeName(recvType)
		if obj := selection.Obj(); obj != nil {
			if f, ok := obj.(*types.Func); ok {
				base.CalleePkg = f.Pkg().Path()
				base.CalleeFile = posFile(fset, f.Pos())
			}
		}
		return []TypedEdge{base}
	}

	// Interface dispatch — resolve to all implementing types.
	base.IsInterface = true
	var edges []TypedEdge
	for _, ct := range allTypes {
		ptr := types.NewPointer(ct)
		if types.Implements(ct, iface) || types.Implements(ptr, iface) {
			edge := base
			edge.ReceiverType = typeName(ct)
			edges = append(edges, edge)
		}
	}

	// If no implementations found, still return the interface edge.
	if len(edges) == 0 {
		base.ReceiverType = typeName(recvType)
		edges = append(edges, base)
	}
	return edges
}

// collectConcreteTypes returns all named concrete types across packages.
func collectConcreteTypes(pkgs []*packages.Package) []types.Type {
	seen := make(map[types.Type]bool)
	var result []types.Type

	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			t := tn.Type()
			if _, isIface := t.Underlying().(*types.Interface); isIface {
				continue
			}
			if !seen[t] {
				seen[t] = true
				result = append(result, t)
			}
		}
	}
	return result
}

// enclosingFunc returns the name of the function containing pos.
func enclosingFunc(fset *token.FileSet, file *ast.File, pos token.Pos) string {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if pos >= fn.Body.Pos() && pos <= fn.Body.End() {
			return fn.Name.Name
		}
	}
	return ""
}

// typeName returns a human-readable name for a type.
func typeName(t types.Type) string {
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	if ptr, ok := t.(*types.Pointer); ok {
		return "*" + typeName(ptr.Elem())
	}
	return t.String()
}

// posFile returns the filename from a token.Pos, or empty if invalid.
func posFile(fset *token.FileSet, pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	return fset.Position(pos).Filename
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -run TestResolve -v -count=1 -timeout 60s`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
cd $REPO_ROOT && git add internal/goanalysis/ && git commit -m "feat: add type-aware call resolver using go/types"
```

---

### Task 4: Converter — TypedEdge to CallEdge Bridge

**Files:**
- Create: `internal/goanalysis/convert.go`
- Create: `internal/goanalysis/convert_test.go`

**Prereqs:** Task 2, Task 3

- [ ] **Step 1: Write the failing test**

```go
// internal/goanalysis/convert_test.go
package goanalysis

import (
	"context"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestConvertToCallGraph(t *testing.T) {
	dir := makeTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := LoadPackages(ctx, dir, LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}

	typedEdges := Resolve(lr.Packages)

	// Create some tree-sitter symbols to merge with.
	tsSymbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 15, EndLine: 17},
		{Name: "greet", Kind: parser.KindFunction, File: "main.go", StartLine: 13, EndLine: 13},
		{Name: "Greet", Kind: parser.KindMethod, File: "main.go", StartLine: 11, EndLine: 11, Receiver: "Hello"},
	}

	cg := ConvertToCallGraph(typedEdges, tsSymbols)
	if cg == nil {
		t.Fatal("expected non-nil CallGraph")
	}
	if len(cg.Edges) == 0 {
		t.Fatal("expected call edges")
	}
	if len(cg.Symbols) == 0 {
		t.Fatal("expected symbols")
	}
}

func TestMergeCallGraphs(t *testing.T) {
	tsGraph := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{
			{Name: "foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1},
		},
		Edges: []callgraph.CallEdge{
			{CalleeName: "bar", Line: 5},
		},
	}
	typedGraph := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{
			{Name: "foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1},
			{Name: "bar", Kind: parser.KindFunction, File: "b.go", StartLine: 1},
		},
		Edges: []callgraph.CallEdge{
			{CalleeName: "bar", Line: 5},
		},
	}

	merged := MergeCallGraphs(tsGraph, typedGraph)
	if merged == nil {
		t.Fatal("expected non-nil merged graph")
	}
	// Typed edges should take priority — merged should have resolved callee.
	if len(merged.Edges) == 0 {
		t.Fatal("expected edges in merged graph")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -run TestConvert -v -count=1 -timeout 60s`
Expected: FAIL — ConvertToCallGraph not defined

- [ ] **Step 3: Write implementation**

```go
// internal/goanalysis/convert.go
package goanalysis

import (
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ConvertToCallGraph converts typed edges to the existing CallGraph format.
// It matches callers/callees against the provided tree-sitter symbols by name+file.
func ConvertToCallGraph(typedEdges []TypedEdge, tsSymbols []*parser.Symbol) *callgraph.CallGraph {
	// Index tree-sitter symbols by name for matching.
	byNameFile := make(map[string][]*parser.Symbol)
	for _, sym := range tsSymbols {
		key := sym.Name + ":" + filepath.Base(sym.File)
		byNameFile[key] = append(byNameFile[key], sym)
		// Also index by name only for cross-file lookups.
		byNameFile[sym.Name] = append(byNameFile[sym.Name], sym)
	}

	edges := make([]callgraph.CallEdge, 0, len(typedEdges))
	for _, te := range typedEdges {
		edge := callgraph.CallEdge{
			CalleeName: te.CalleeName,
			Receiver:   te.ReceiverType,
			Line:       te.Line,
		}

		// Resolve caller against tree-sitter symbols.
		if te.CallerFile != "" {
			key := te.CallerName + ":" + filepath.Base(te.CallerFile)
			if syms := byNameFile[key]; len(syms) > 0 {
				edge.Caller = syms[0]
			}
		}
		if edge.Caller == nil {
			if syms := byNameFile[te.CallerName]; len(syms) > 0 {
				edge.Caller = syms[0]
			}
		}

		// Resolve callee.
		if te.CalleeFile != "" {
			key := te.CalleeName + ":" + filepath.Base(te.CalleeFile)
			if syms := byNameFile[key]; len(syms) > 0 {
				edge.Callee = syms[0]
			}
		}
		if edge.Callee == nil {
			if syms := byNameFile[te.CalleeName]; len(syms) > 0 {
				edge.Callee = syms[0]
			}
		}

		edges = append(edges, edge)
	}

	return &callgraph.CallGraph{
		Edges:   edges,
		Symbols: tsSymbols,
	}
}

// MergeCallGraphs combines a tree-sitter call graph with a typed call graph.
// Typed edges take priority: if a typed edge matches a tree-sitter edge
// (same caller+callee+line), the typed version replaces it. Unmatched
// tree-sitter edges are kept for non-Go files or unresolved calls.
func MergeCallGraphs(tsGraph, typedGraph *callgraph.CallGraph) *callgraph.CallGraph {
	if typedGraph == nil {
		return tsGraph
	}
	if tsGraph == nil {
		return typedGraph
	}

	// Index typed edges by caller+line for dedup.
	typedSet := make(map[string]bool)
	for _, e := range typedGraph.Edges {
		key := edgeKey(e)
		typedSet[key] = true
	}

	// Start with all typed edges.
	merged := make([]callgraph.CallEdge, 0, len(typedGraph.Edges)+len(tsGraph.Edges))
	merged = append(merged, typedGraph.Edges...)

	// Add tree-sitter edges that don't have typed equivalents.
	for _, e := range tsGraph.Edges {
		if !typedSet[edgeKey(e)] {
			merged = append(merged, e)
		}
	}

	// Merge symbol lists (dedup by name+file).
	symbols := mergeSymbols(tsGraph.Symbols, typedGraph.Symbols)

	return &callgraph.CallGraph{
		Edges:         merged,
		Symbols:       symbols,
		HookCallbacks: tsGraph.HookCallbacks,
	}
}

func edgeKey(e callgraph.CallEdge) string {
	callerName := ""
	if e.Caller != nil {
		callerName = e.Caller.Name
	}
	return callerName + "->" + e.CalleeName
}

func mergeSymbols(a, b []*parser.Symbol) []*parser.Symbol {
	seen := make(map[string]bool)
	var result []*parser.Symbol
	for _, sym := range a {
		key := sym.Name + ":" + sym.File
		if !seen[key] {
			seen[key] = true
			result = append(result, sym)
		}
	}
	for _, sym := range b {
		key := sym.Name + ":" + sym.File
		if !seen[key] {
			seen[key] = true
			result = append(result, sym)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/goanalysis/ -run "TestConvert|TestMerge" -v -count=1 -timeout 60s`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
cd $REPO_ROOT && git add internal/goanalysis/ && git commit -m "feat: add TypedEdge to CallGraph converter and graph merger"
```

---

## Chunk 2: Integration — Wire Go Analysis into Existing Tools

### Task 5: Enhance BuildFromRepo with Type-Aware Resolution

**Files:**
- Modify: `internal/callgraph/repo.go`
- Create: `internal/callgraph/repo_test.go`

**Prereqs:** Tasks 2, 3, 4

- [ ] **Step 1: Write the failing test**

```go
// internal/callgraph/repo_test.go
package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildFromRepo_GoTypesEnhanced(t *testing.T) {
	dir := makeGoTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cg, err := BuildFromRepo(ctx, TraceRepoInput{
		Root:     dir,
		Language: "go",
	})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if cg == nil {
		t.Fatal("expected non-nil call graph")
	}
	if len(cg.Edges) == 0 {
		t.Fatal("expected call edges")
	}
	// Check tier info is populated.
	if cg.Tier == "" {
		t.Error("expected tier to be set")
	}
}

func TestBuildFromRepo_FallbackToTreeSitter(t *testing.T) {
	// Python repo — no go.mod → should fall back to tree-sitter.
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.py"), `
def foo():
    bar()

def bar():
    pass
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cg, err := BuildFromRepo(ctx, TraceRepoInput{
		Root:     dir,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if cg.Tier != "basic" {
		t.Errorf("expected basic tier for python, got %s", cg.Tier)
	}
}

func makeGoTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

type Runner interface { Run() }
type Impl struct{}
func (Impl) Run() {}
func execute(r Runner) { r.Run() }
func main() { execute(Impl{}) }
`)
	return dir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/callgraph/ -run "TestBuildFromRepo_Go|TestBuildFromRepo_Fallback" -v -count=1 -timeout 60s`
Expected: FAIL — cg.Tier undefined (field not yet added)

- [ ] **Step 3: Modify CallGraph struct and BuildFromRepo**

Add `Tier` field to `CallGraph` in `graph.go`:

```go
// In internal/callgraph/graph.go, add to CallGraph struct:
Tier string // "basic", "enhanced", or "full" — analysis precision level
```

Modify `BuildFromRepo` in `repo.go` to attempt go/types resolution for Go repos:

```go
// In internal/callgraph/repo.go, add import and modify BuildFromRepo:

import (
	// existing imports...
	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
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

	// Build tree-sitter call graph (baseline for all languages).
	cg := BuildCallGraph(allSymbols, allCalls)
	cg.Tier = "basic"

	// Attempt type-aware enhancement for Go repos.
	if goanalysis.HasGoModule(input.Root) {
		typedCG := tryGoTypesResolution(ctx, input.Root, allSymbols)
		if typedCG != nil {
			cg = goanalysis.MergeCallGraphs(cg, typedCG)
			cg.Tier = "enhanced"
		}
	}

	// Inject WordPress hook edges for PHP files.
	hookRoutes := extractHookRoutes(ir.Files)
	if len(hookRoutes) > 0 {
		InjectHookEdges(cg, hookRoutes)
	}

	return cg, nil
}

// tryGoTypesResolution attempts to load Go packages and resolve calls.
// Returns nil on any failure (graceful degradation).
func tryGoTypesResolution(ctx context.Context, root string, tsSymbols []*parser.Symbol) *CallGraph {
	lr, err := goanalysis.LoadPackages(ctx, root, goanalysis.LoadOpts{})
	if err != nil {
		return nil
	}
	typedEdges := goanalysis.Resolve(lr.Packages)
	if len(typedEdges) == 0 {
		return nil
	}
	return goanalysis.ConvertToCallGraph(typedEdges, tsSymbols)
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/callgraph/ -run "TestBuildFromRepo" -v -count=1 -timeout 60s`
Expected: PASS (2 tests)

- [ ] **Step 5: Run all existing tests to verify no regressions**

Run: `cd $REPO_ROOT && GOWORK=off go test ./... -count=1 -timeout 120s`
Expected: All existing tests pass

- [ ] **Step 6: Commit**

```bash
cd $REPO_ROOT && git add internal/callgraph/ && git commit -m "feat: integrate go/types resolution into BuildFromRepo with graceful fallback"
```

---

### Task 6: Add Tier/Provenance to XML Output

**Files:**
- Modify: `cmd/go-code/tool_call_trace.go` — add tier + provenance to response
- Modify: `cmd/go-code/tool_impact.go` — add tier to response
- Modify: `cmd/go-code/tool_dead_code.go` — add tier to response

**Prereqs:** Task 5

- [ ] **Step 1: Add provenance XML types to helpers.go**

In `cmd/go-code/helpers.go`, add:

```go
type xmlProvenance struct {
	Tier     string   `xml:"tier,attr"`
	Backends []string `xml:"backend,omitempty"`
}

type xmlWarning struct {
	Code          string `xml:"code,attr"`
	Message       string `xml:"message,attr"`
	CapabilityPct int    `xml:"capabilityPct,attr"`
}
```

- [ ] **Step 2: Add tier to xmlTrace struct in tool_call_trace.go**

Add to `xmlTrace` struct:
```go
Tier     string       `xml:"tier,attr,omitempty"`
Warnings []xmlWarning `xml:"warning,omitempty"`
```

Populate in `handleCallTrace`:
```go
resp.Trace.Tier = result.Tier // from TraceResult, which gets it from CallGraph
```

Where `TraceResult` needs `Tier` field. Add to `internal/callgraph/trace.go` `TraceResult`:
```go
Tier string `json:"tier,omitempty"`
```

And in `Trace()`, set `result.Tier = g.Tier` (pass from CallGraph).

- [ ] **Step 3: Add tier to impact output**

In `handleImpact`, add tier from call graph:
```go
// After building cg:
output.Tier = cg.Tier
```

Add `Tier string` field to `impactOutput` struct.

- [ ] **Step 4: Add tier to dead_code response**

In `xmlDeadCode` struct, add:
```go
Tier string `xml:"tier,attr,omitempty"`
```

Populate from `cg.Tier` in `handleDeadCode`.

- [ ] **Step 5: Run all tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./... -count=1 -timeout 120s`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
cd $REPO_ROOT && git add cmd/go-code/ internal/callgraph/ && git commit -m "feat: add tier and provenance metadata to call_trace, impact, dead_code outputs"
```

---

## Chunk 3: Compound Tools

### Task 7: `understand` Compound Tool

**Files:**
- Create: `internal/compound/understand.go`
- Create: `internal/compound/understand_test.go`
- Create: `cmd/go-code/tool_understand.go`

**Prereqs:** Tasks 1, 5

- [ ] **Step 1: Write the failing test**

```go
// internal/compound/understand_test.go
package compound

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestUnderstand_Basic(t *testing.T) {
	sym := &parser.Symbol{
		Name: "Foo", Kind: parser.KindFunction,
		File: "main.go", StartLine: 10, EndLine: 20,
		Signature: "func Foo(x int) error", Complexity: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym},
		Edges:   []callgraph.CallEdge{{Caller: sym, CalleeName: "bar", Line: 12}},
		Tier:    "basic",
	}

	result := Understand(sym, cg, UnderstandOpts{})
	if result.Symbol.Name != "Foo" {
		t.Errorf("expected Foo, got %s", result.Symbol.Name)
	}
	if result.Tier != "basic" {
		t.Errorf("expected basic tier, got %s", result.Tier)
	}
	if len(result.Callees) == 0 {
		t.Error("expected callees")
	}
}

func TestUnderstand_Ambiguous(t *testing.T) {
	sym1 := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1}
	sym2 := &parser.Symbol{Name: "Foo", Kind: parser.KindMethod, File: "b.go", StartLine: 5, Receiver: "Bar"}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym1, sym2},
		Tier:    "basic",
	}

	result := FindSymbol(cg.Symbols, "Foo")
	if len(result) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result))
	}
}

func TestUnderstand_WithCallers(t *testing.T) {
	foo := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1, EndLine: 10}
	bar := &parser.Symbol{Name: "Bar", Kind: parser.KindFunction, File: "b.go", StartLine: 1, EndLine: 5}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{foo, bar},
		Edges:   []callgraph.CallEdge{{Caller: bar, Callee: foo, CalleeName: "Foo", Line: 3}},
		Tier:    "enhanced",
	}

	result := Understand(foo, cg, UnderstandOpts{IncludeCallers: true})
	if len(result.Callers) == 0 {
		t.Error("expected callers")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/compound/ -v -count=1`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

```go
// internal/compound/understand.go
package compound

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// UnderstandOpts configures the understand compound analysis.
type UnderstandOpts struct {
	IncludeCallers bool
	MaxCallees     int // default 20
	MaxCallers     int // default 20
}

// UnderstandResult is the output of the understand compound tool.
type UnderstandResult struct {
	Symbol     SymbolInfo    `json:"symbol"`
	Callees    []CallRef     `json:"callees,omitempty"`
	Callers    []CallRef     `json:"callers,omitempty"`
	Tier       string        `json:"tier"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// SymbolInfo is a summary of a symbol for compound tool output.
type SymbolInfo struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	StartLine  uint32 `json:"start_line"`
	EndLine    uint32 `json:"end_line"`
	Signature  string `json:"signature,omitempty"`
	Complexity int    `json:"complexity,omitempty"`
	Receiver   string `json:"receiver,omitempty"`
}

// CallRef is a reference to a called/calling function.
type CallRef struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Line     uint32 `json:"line"`
	Receiver string `json:"receiver,omitempty"`
}

// FindSymbol returns all function/method symbols matching the given name.
func FindSymbol(symbols []*parser.Symbol, name string) []*parser.Symbol {
	var matches []*parser.Symbol
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			matches = append(matches, sym)
		}
	}
	return matches
}

// Understand performs a deep-dive analysis of a single symbol.
// Combines: symbol info + callees + callers from the call graph.
func Understand(sym *parser.Symbol, cg *callgraph.CallGraph, opts UnderstandOpts) *UnderstandResult {
	maxCallees := opts.MaxCallees
	if maxCallees <= 0 {
		maxCallees = 20
	}
	maxCallers := opts.MaxCallers
	if maxCallers <= 0 {
		maxCallers = 20
	}

	result := &UnderstandResult{
		Symbol: SymbolInfo{
			Name:       sym.Name,
			Kind:       string(sym.Kind),
			File:       sym.File,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			Signature:  sym.Signature,
			Complexity: sym.Complexity,
			Receiver:   sym.Receiver,
		},
		Tier: cg.Tier,
	}

	// Collect callees (what does this symbol call?).
	for _, e := range cg.Edges {
		if e.Caller == sym && len(result.Callees) < maxCallees {
			ref := CallRef{
				Name:     e.CalleeName,
				Line:     e.Line,
				Receiver: e.Receiver,
			}
			if e.Callee != nil {
				ref.File = e.Callee.File
			}
			result.Callees = append(result.Callees, ref)
		}
	}

	// Collect callers (who calls this symbol?).
	if opts.IncludeCallers {
		for _, e := range cg.Edges {
			if e.Callee == sym && len(result.Callers) < maxCallers {
				ref := CallRef{
					Name: e.CalleeName,
					Line: e.Line,
				}
				if e.Caller != nil {
					ref.Name = e.Caller.Name
					ref.File = e.Caller.File
				}
				result.Callers = append(result.Callers, ref)
			}
		}
	}

	return result
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/compound/ -v -count=1`
Expected: PASS (3 tests)

- [ ] **Step 5: Create MCP tool handler**

```go
// cmd/go-code/tool_understand.go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UnderstandInput is the input schema for the understand tool.
type UnderstandInput struct {
	Repo           string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol         string `json:"symbol" jsonschema_description:"Function or method name to analyze in depth"`
	Focus          string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope"`
	Language       string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
	IncludeCallers bool   `json:"include_callers,omitempty" jsonschema_description:"Include who calls this symbol (default: false, saves time)"`
}

func registerUnderstand(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "understand",
		Description: "Deep-dive into a single symbol. " +
			"Aggregates: symbol info + callees + callers + complexity. " +
			"Returns type-aware results for Go repos (interface dispatch resolution). " +
			"Use instead of separate call_trace + symbol_search + code_graph calls. " +
			"Suggests similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UnderstandInput) (*mcp.CallToolResult, error) {
		return handleUnderstand(ctx, input, deps, sem)
	})
}

func handleUnderstand(ctx context.Context, input UnderstandInput, deps analyze.Deps, sem *SemanticDeps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	// Find matching symbols.
	matches := compound.FindSymbol(cg.Symbols, input.Symbol)
	if len(matches) == 0 {
		msg := fmt.Sprintf("symbol %q not found", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(fmt.Sprintf("<response tool=\"understand\">\n  <error>%s</error>\n%s\n</response>",
				escapeXML(msg), suggestions)), nil
		}
		return errResult(msg), nil
	}

	// If ambiguous, return disambiguation hint.
	if len(matches) > 1 {
		type hint struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			File     string `json:"file"`
			Line     uint32 `json:"line"`
			Receiver string `json:"receiver,omitempty"`
		}
		hints := make([]hint, len(matches))
		for i, m := range matches {
			hints[i] = hint{Name: m.Name, Kind: string(m.Kind), File: m.File, Line: m.StartLine, Receiver: m.Receiver}
		}
		data, _ := json.MarshalIndent(struct {
			Message string `json:"message"`
			Matches []hint `json:"matches"`
		}{
			Message: fmt.Sprintf("ambiguous: %d symbols match %q — specify file or receiver to disambiguate", len(matches), input.Symbol),
			Matches: hints,
		}, "", "  ")
		return textResult(string(data)), nil
	}

	result := compound.Understand(matches[0], cg, compound.UnderstandOpts{
		IncludeCallers: input.IncludeCallers,
	})

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(data)), nil
}
```

- [ ] **Step 6: Register tool in register.go**

Add `registerUnderstand(server, cfg, deps, &semDeps)` after `registerImpact` in `register.go`.

- [ ] **Step 7: Run all tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./... -count=1 -timeout 120s`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
cd $REPO_ROOT && git add internal/compound/ cmd/go-code/tool_understand.go cmd/go-code/register.go && git commit -m "feat: add understand compound tool — aggregates symbol info, callees, callers"
```

---

### Task 8: `prepare_change` Compound Tool

**Files:**
- Create: `internal/compound/prepare_change.go`
- Create: `internal/compound/prepare_change_test.go`
- Create: `cmd/go-code/tool_prepare_change.go`

**Prereqs:** Tasks 5, 7

- [ ] **Step 1: Write the failing test**

```go
// internal/compound/prepare_change_test.go
package compound

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestPrepareChange_Basic(t *testing.T) {
	foo := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1, EndLine: 10}
	bar := &parser.Symbol{Name: "Bar", Kind: parser.KindFunction, File: "b.go", StartLine: 1, EndLine: 5}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{foo, bar},
		Edges:   []callgraph.CallEdge{{Caller: bar, Callee: foo, CalleeName: "Foo", Line: 3}},
		Tier:    "basic",
	}

	result := PrepareChange(cg, "Foo", PrepareChangeOpts{})
	if !result.Found {
		t.Fatal("expected symbol to be found")
	}
	if result.Impact == nil {
		t.Fatal("expected impact result")
	}
	if result.Impact.TotalAffected == 0 {
		t.Error("expected affected callers")
	}
	if result.Tier != "basic" {
		t.Errorf("expected basic tier, got %s", result.Tier)
	}
}

func TestPrepareChange_NotFound(t *testing.T) {
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{},
		Tier:    "basic",
	}

	result := PrepareChange(cg, "Missing", PrepareChangeOpts{})
	if result.Found {
		t.Fatal("expected symbol not found")
	}
}

func TestPrepareChange_IsDead(t *testing.T) {
	foo := &parser.Symbol{Name: "foo", Kind: parser.KindFunction, File: "a.go", StartLine: 1, EndLine: 5}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{foo},
		Tier:    "basic",
	}

	result := PrepareChange(cg, "foo", PrepareChangeOpts{})
	if !result.IsDead {
		t.Error("expected foo to be detected as dead code")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/compound/ -run TestPrepareChange -v -count=1`
Expected: FAIL — PrepareChange not defined

- [ ] **Step 3: Write implementation**

```go
// internal/compound/prepare_change.go
package compound

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/impact"
)

// PrepareChangeOpts configures pre-change analysis.
type PrepareChangeOpts struct {
	MaxDepth int // max impact traversal depth (default 5)
}

// PrepareChangeResult is the output of pre-change risk assessment.
type PrepareChangeResult struct {
	Found    bool           `json:"found"`
	Symbol   SymbolInfo     `json:"symbol,omitempty"`
	Impact   *impact.Result `json:"impact,omitempty"`
	IsDead   bool           `json:"is_dead"`
	DeadCode *deadcode.Result `json:"dead_code_summary,omitempty"`
	Tier     string         `json:"tier"`
	Warnings []string       `json:"warnings,omitempty"`
}

// PrepareChange assesses the risk of changing a symbol.
// Aggregates: impact analysis + dead code check.
func PrepareChange(cg *callgraph.CallGraph, symbolName string, opts PrepareChangeOpts) *PrepareChangeResult {
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}

	result := &PrepareChangeResult{Tier: cg.Tier}

	// 1. Impact analysis.
	impactResult := impact.Analyze(cg, symbolName, impact.Options{MaxDepth: maxDepth})
	result.Impact = impactResult
	result.Found = impactResult.Found

	if !result.Found {
		return result
	}

	// Populate symbol info from the symbol table.
	for _, sym := range cg.Symbols {
		if sym.Name == symbolName {
			result.Symbol = SymbolInfo{
				Name:       sym.Name,
				Kind:       string(sym.Kind),
				File:       sym.File,
				StartLine:  sym.StartLine,
				EndLine:    sym.EndLine,
				Signature:  sym.Signature,
				Complexity: sym.Complexity,
				Receiver:   sym.Receiver,
			}
			break
		}
	}

	// 2. Dead code check — is this symbol even used?
	dcResult := deadcode.Analyze(cg, deadcode.Options{IncludeExported: true, HookCallbacks: cg.HookCallbacks})
	result.DeadCode = dcResult
	for _, ds := range dcResult.DeadSymbols {
		if ds.Name == symbolName {
			result.IsDead = true
			break
		}
	}

	return result
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./internal/compound/ -run TestPrepareChange -v -count=1`
Expected: PASS (3 tests)

- [ ] **Step 5: Create MCP tool handler**

```go
// cmd/go-code/tool_prepare_change.go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PrepareChangeInput is the input schema for the prepare_change tool.
type PrepareChangeInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol   string `json:"symbol" jsonschema_description:"Function or method name to assess change risk for"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Max impact traversal depth (default 5, max 10)"`
}

func registerPrepareChange(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "prepare_change",
		Description: "Pre-change risk assessment for a function or method. " +
			"Aggregates: impact_analysis (blast radius, affected callers) + dead_code (is it even used?). " +
			"Returns risk level, affected packages, and dead code status. " +
			"Use before refactoring to understand what might break. " +
			"Suggests similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareChangeInput) (*mcp.CallToolResult, error) {
		return handlePrepareChange(ctx, input, deps, sem)
	})
}

func handlePrepareChange(ctx context.Context, input PrepareChangeInput, deps analyze.Deps, sem *SemanticDeps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	result := compound.PrepareChange(cg, input.Symbol, compound.PrepareChangeOpts{
		MaxDepth: input.Depth,
	})

	if !result.Found {
		msg := fmt.Sprintf("symbol %q not found", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(fmt.Sprintf("<response tool=\"prepare_change\">\n  <error>%s</error>\n%s\n</response>",
				escapeXML(msg), suggestions)), nil
		}
		return errResult(msg), nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(data)), nil
}
```

- [ ] **Step 6: Register in register.go**

Add `registerPrepareChange(server, cfg, deps, &semDeps)` after `registerUnderstand` in `register.go`.

- [ ] **Step 7: Run all tests**

Run: `cd $REPO_ROOT && GOWORK=off go test ./... -count=1 -timeout 120s`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
cd $REPO_ROOT && git add internal/compound/ cmd/go-code/tool_prepare_change.go cmd/go-code/register.go && git commit -m "feat: add prepare_change compound tool — aggregates impact analysis + dead code check"
```

---

## Chunk 4: Polish, Lint, Deploy

### Task 9: Lint + go.sum + Update CLAUDE.md + Update ROADMAP

**Files:**
- Modify: `go.mod` (add x/tools dependency)
- Modify: `CLAUDE.md` (add new tools, update tool count)
- Modify: `docs/ROADMAP.md` (mark v1.18 complete)

**Prereqs:** All previous tasks

- [ ] **Step 1: Tidy go.mod and update go.sum**

Run: `cd $REPO_ROOT && GOWORK=off go mod tidy`

- [ ] **Step 2: Run lint**

Run: `cd $REPO_ROOT && GOWORK=off make lint`
Expected: Clean or fix any issues

- [ ] **Step 3: Run full test suite**

Run: `cd $REPO_ROOT && GOWORK=off go test ./... -count=1 -race -timeout 120s`
Expected: All tests pass with race detector

- [ ] **Step 4: Update CLAUDE.md**

Add `understand` and `prepare_change` to the MCP Tools table. Update tool count from 16 to 18. Add `internal/goanalysis/` to Package Overview. Add `internal/tier/` and `internal/compound/` to Package Overview.

- [ ] **Step 5: Update ROADMAP.md**

Mark v1.18 as complete. Update the dependency graph. Add release entry.

- [ ] **Step 6: Commit**

```bash
cd $REPO_ROOT && git add -A && git commit -m "chore: lint, tidy, update docs for v1.18"
```

---

### Task 10: Deploy and Verify

**Files:** None (deploy only)

**Prereqs:** Task 9

- [ ] **Step 1: Build Docker image**

Run: `cd ~/deploy/my-server && docker compose build --no-cache go-code`
Expected: Build succeeds

- [ ] **Step 2: Deploy**

Run: `cd ~/deploy/my-server && docker compose up -d --no-deps --force-recreate go-code`
Expected: Container starts, health check passes

- [ ] **Step 3: Verify health**

Run: `curl -s http://127.0.0.1:8897/health | jq .`
Expected: `{"status":"ok"}`

- [ ] **Step 4: E2E test — understand tool on local repo**

Test via MCP: `understand` tool with `repo=$REPO_ROOT`, `symbol=BuildCallGraph`
Expected: Returns symbol info + callees + tier

- [ ] **Step 5: E2E test — prepare_change tool**

Test via MCP: `prepare_change` tool with `repo=$REPO_ROOT`, `symbol=ParseFile`
Expected: Returns impact analysis + dead code status + tier

- [ ] **Step 6: E2E test — call_trace with tier info**

Test via MCP: `call_trace` with `repo=$REPO_ROOT`, `symbol=BuildCallGraph`, `compact=true`
Expected: Response includes `tier="enhanced"` (since go-code has go.mod)

---

## Task Dependency Graph

```
Task 1 (tier) ──────────────────────────────────┐
Task 2 (loader) ──→ Task 3 (resolver) ──→ Task 4 (converter) ──→ Task 5 (integration) ──→ Task 6 (XML output)
                                                                        │
                                                                Task 7 (understand) ──→ Task 8 (prepare_change)
                                                                        │
                                                                Task 9 (lint/docs) ──→ Task 10 (deploy)
```

**Parallel groups:**
- **Group A** (independent): Tasks 1, 2
- **Group B** (depends on 2): Task 3
- **Group C** (depends on 2, 3): Task 4
- **Group D** (depends on 1-4): Task 5
- **Group E** (depends on 5): Tasks 6, 7 (parallel)
- **Group F** (depends on 7): Task 8
- **Group G** (depends on all): Tasks 9, 10 (sequential)
