package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

// Hard red tests — edge cases for type-aware resolution.

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_EmptyPackages(t *testing.T) {
	edges := goanalysis.Resolve(nil)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for nil packages, got %d", len(edges))
	}
}

func TestResolve_ClosureCalls(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/closure\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func main() {
	fn := func(x int) int { return x + 1 }
	_ = fn(42)
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)
	_ = edges // verify no panic
}

func TestResolve_EmbeddedInterface(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/embed\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Reader interface { Read() string }
type Writer interface { Write(s string) }
type ReadWriter interface {
	Reader
	Writer
}

type File struct{ name string }
func (f File) Read() string { return f.name }
func (f File) Write(s string) { f.name = s }

func process(rw ReadWriter) { fmt.Println(rw.Read()) }

func main() { process(File{name: "test"}) }
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "process" && e.CalleeName == "Read" && e.IsInterface {
			found = true
			if e.ReceiverType != "File" {
				t.Errorf("expected receiver File, got %q", e.ReceiverType)
			}
			break
		}
	}
	if !found {
		t.Error("expected interface dispatch process -> Read resolved to File")
	}
}

func TestResolve_PointerReceiver(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/ptr\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

type Counter struct{ n int }
func (c *Counter) Inc() { c.n++ }

func main() {
	c := &Counter{}
	c.Inc()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "Inc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected main -> Inc edge for pointer receiver call")
	}
}

func TestResolve_MultipleInterfacesSameMethod(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/multi\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Stringer interface { String() string }
type Namer interface { String() string }

type Person struct{ Name string }
func (p Person) String() string { return p.Name }

func printStr(s Stringer) { fmt.Println(s.String()) }
func printName(n Namer) { fmt.Println(n.String()) }

func main() {
	p := Person{Name: "Alice"}
	printStr(p)
	printName(p)
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	strCount := 0
	nameCount := 0
	for _, e := range edges {
		if e.CallerName == "printStr" && e.CalleeName == "String" && e.IsInterface {
			strCount++
		}
		if e.CallerName == "printName" && e.CalleeName == "String" && e.IsInterface {
			nameCount++
		}
	}
	if strCount == 0 {
		t.Error("expected interface dispatch from printStr")
	}
	if nameCount == 0 {
		t.Error("expected interface dispatch from printName")
	}
}

func TestResolve_NoMethodsNoPanic(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/nomethod\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func add(a, b int) int { return a + b }
func main() { _ = add(1, 2) }
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "add" {
			found = true
			if e.IsInterface {
				t.Error("add() is not an interface dispatch")
			}
			break
		}
	}
	if !found {
		t.Error("expected main -> add edge")
	}
}

// Func-value-alias fixtures — P3 of the callgraph-seam unification plan
// (2026-07-02, Track-2/BUG B): a package-level var whose single static
// initializer is itself a function-valued expression (a bare func ident or a
// method-value selector) was previously invisible to resolveIdent/
// resolveSelector — `obj.(*types.Var)` fell through to `return nil` at
// resolver_dispatch.go:17-19, silently dropping the CALLS edge for every call
// site that goes through the var instead of the function's own name. This is
// the exact live-repro shape at internal/callgraph/eager_warm.go:31
// (`var recordEagerWarmFn = recordEagerWarm`), which false-dead-flagged
// recordEagerWarm despite 6 real callers (krolik-server go-code
// repo-review-council 2026-07-01.md HIGH finding).
//
// This is "fixture (b)" at the goanalysis.Resolve layer — the resolver-level
// gate this task closes; internal/callgraph/repo_test.go's
// TestBuildCallGraph_VarFuncBindingCalleeUnresolved exercises the SAME shape
// one layer down at the untyped tree-sitter-only callgraph.BuildCallGraph
// seam, which this task deliberately does not touch (zero tree-sitter-builder
// changes) and which stays RED by design — proof the two bugs are separable
// (BuildFromRepo/EnrichWithTypedResolution merges this package's typed edges
// on top of the untyped base via MergeCallGraphs; raw BuildCallGraph alone
// never gets that merge).

// TestResolve_VarFuncBindingAlias is fixture (b): a bare-ident call through a
// package-level var-func-binding (`workFn()` where `var workFn = realWork`)
// must resolve to realWork, mirroring recordEagerWarmFn/recordEagerWarm.
func TestResolve_VarFuncBindingAlias(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/varfuncbinding\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func realWork() int { return 42 }

var workFn = realWork

func UseWorkFn() int {
	return workFn()
}

func main() {
	_ = UseWorkFn()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "UseWorkFn" && e.CalleeName == "realWork" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UseWorkFn -> realWork edge via the workFn var-func-binding alias; got edges: %v", summarizeTyped(edges))
	}
}

// TestResolve_MethodValueBindingAlias is the method-value-dispatch half of
// the same shape class: a package-level var whose single static initializer
// is a bound method value (`var greetFn = defaultGreeter.Greet`) must resolve
// through the var to the underlying method.
func TestResolve_MethodValueBindingAlias(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/methodvalue\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

type Greeter struct{ name string }

func (g Greeter) Greet() string { return "hi " + g.name }

var defaultGreeter = Greeter{name: "world"}
var greetFn = defaultGreeter.Greet

func UseGreetFn() string {
	return greetFn()
}

func main() {
	_ = UseGreetFn()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "UseGreetFn" && e.CalleeName == "Greet" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UseGreetFn -> Greet edge via the greetFn method-value alias; got edges: %v", summarizeTyped(edges))
	}
}

// TestResolve_QualifiedVarFuncBindingAlias proves the same shape resolves
// through resolveSelector's "qualified name via Uses" branch too, for a
// cross-package call site (`otherpkg.WorkFn()`), not just resolveIdent's
// bare-ident branch.
func TestResolve_QualifiedVarFuncBindingAlias(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/qualifiedvarfuncbinding\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "worker", "worker.go"), `package worker

func RealWork() int { return 42 }

var WorkFn = RealWork
`)
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "example.com/qualifiedvarfuncbinding/worker"

func UseWorkFn() int {
	return worker.WorkFn()
}

func main() {
	_ = UseWorkFn()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "UseWorkFn" && e.CalleeName == "RealWork" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UseWorkFn -> RealWork edge via the worker.WorkFn qualified var-func-binding alias; got edges: %v", summarizeTyped(edges))
	}
}

// TestResolve_ReassignedVarFuncBinding_NoEdge proves the conservatism half of
// the acceptance criteria: a var whose binding is reassigned elsewhere (so a
// single static initializer no longer describes every call through it) must
// resolve to NO edge at all, exactly like today's behavior for any other
// ambiguous/unresolved callee — never a wrong edge to either candidate.
func TestResolve_ReassignedVarFuncBinding_NoEdge(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/reassigned\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func realWork() int { return 1 }
func otherWork() int { return 2 }

var workFn = realWork

func rebind() {
	workFn = otherWork
}

func UseWorkFn() int {
	return workFn()
}

func main() {
	rebind()
	_ = UseWorkFn()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	for _, e := range edges {
		if e.CallerName == "UseWorkFn" && (e.CalleeName == "realWork" || e.CalleeName == "otherWork") {
			t.Errorf("expected NO edge for a reassigned var-func-binding (ambiguous); got %v", summarizeTyped(edges))
		}
	}
}

// TestResolve_ParallelVarDecl_NoEdge proves the second conservatism case: a
// parallel/multi-value var declaration (`var a, b = f, g`) never qualifies as
// a single static initializer, so neither call resolves through the alias.
func TestResolve_ParallelVarDecl_NoEdge(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/parallel\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func realWork() int { return 1 }
func otherWork() int { return 2 }

var workFn, otherFn = realWork, otherWork

func UseBoth() int {
	return workFn() + otherFn()
}

func main() {
	_ = UseBoth()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	for _, e := range edges {
		if e.CallerName == "UseBoth" && (e.CalleeName == "realWork" || e.CalleeName == "otherWork") {
			t.Errorf("expected NO edge for a parallel var decl (ambiguous, not a single static initializer); got %v", summarizeTyped(edges))
		}
	}
}
