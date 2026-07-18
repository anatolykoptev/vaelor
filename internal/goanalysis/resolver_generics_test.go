package goanalysis_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

// Generic-instantiation caller resolution — go-code's own callgraph resolver
// blind spot: a generic function (Fn[T] / pkg.Fn[T1, T2]) instantiated inside
// a package-level var/const composite-literal initializer produced ZERO
// caller edges, because (a) extractFileEdges' callee switch never unwrapped
// *ast.IndexExpr / *ast.IndexListExpr down to the underlying Ident/Selector,
// and (b) enclosingFunc only matched *ast.FuncDecl bodies, so a call site
// inside a package-level var initializer got callerName="" (nil Caller
// downstream). Confirmed prod repro: internal/cache/lru.go's NewLRU[K, V]
// has 5 real callers, all `var g = &T{lru: cache.NewLRU[string, X](n)}` —
// understand()/call_trace() reported 0 callers despite code_search finding
// "NewLRU[" in all 5 files.

// TestResolve_GenericFuncCall_PackageVarInit is the NewLRU-shaped gate case:
// a generic function with 2 type params, instantiated via a package-qualified
// selector, nested inside a composite-literal field value of a package-level
// var initializer. Before the fix this produced 0 edges (0 callers). After
// the fix it must resolve to the var's declared name as caller.
func TestResolve_GenericFuncCall_PackageVarInit(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/genvar\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "cache", "lru.go"), `package cache

type LRU[K comparable, V any] struct {
	maxSize int
}

func NewLRU[K comparable, V any](maxSize int) *LRU[K, V] {
	return &LRU[K, V]{maxSize: maxSize}
}
`)
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "example.com/genvar/cache"

type entry struct {
	data string
}

type holder struct {
	lru *cache.LRU[string, entry]
}

var globalHolder = &holder{
	lru: cache.NewLRU[string, entry](10),
}

func main() {
	_ = globalHolder
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	var found *goanalysis.TypedEdge
	for i := range edges {
		if edges[i].CalleeName == "NewLRU" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected >=1 caller edge for NewLRU (package-level var-init generic instantiation), got 0; edges: %v", summarizeTyped(edges))
	}
	if found.CallerName != "globalHolder" {
		t.Errorf("expected caller name %q (the package-level var), got %q", "globalHolder", found.CallerName)
	}
	if found.CallerFile == "" {
		t.Error("expected non-empty CallerFile for the var-init caller")
	}
	if found.CallerLine == 0 {
		t.Error("expected non-zero CallerLine for the var-init caller")
	}
}

// TestResolve_GenericFuncCall_InFuncBody is the ordinary-context regression
// guard: a single-type-param generic call (*ast.IndexExpr, not
// *ast.IndexListExpr) from inside a normal function body. Also 0 edges
// before the fix — proves the IndexExpr/IndexListExpr unwrap is required
// independent of the var-init caller-context bug.
func TestResolve_GenericFuncCall_InFuncBody(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/genbody\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func identity[T any](v T) T { return v }

func main() {
	_ = identity[int](42)
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	if !hasEdge(edgePtrs(edges), "main", "identity") {
		t.Errorf("expected main -> identity edge (generic call in func body); got: %v", summarize(edgePtrs(edges)))
	}
}

func summarizeTyped(edges []goanalysis.TypedEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.CallerName+" -> "+e.CalleeName)
	}
	return out
}
