package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"golang.org/x/tools/go/packages"
)

// makeTwoPackageModule builds a module whose main package imports a second
// package in the same module and calls through an interface, so the load
// produces a real dependency package AND populates Uses/Defs/Selections.
func makeTwoPackageModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module example.com/testmod\n\ngo 1.21\n")
	write("greet/greet.go", `package greet

// Greeter defines a greeting interface.
type Greeter interface {
	Greet(name string) string
}

// Simple implements Greeter.
type Simple struct {
	Prefix string
}

// Greet returns a greeting string.
func (g *Simple) Greet(name string) string {
	return g.Prefix + name
}

// Default exists so the loaded package has a package-level initializer and
// types.Info.InitOrder is non-empty — without it the InitOrder assertion below
// would pass whether or not the map was ever released.
var Default = &Simple{Prefix: "hi "}
`)
	write("main.go", `package main

import (
	"strings"

	"example.com/testmod/greet"
)

func main() {
	var g greet.Greeter = &greet.Simple{Prefix: "hi "}
	_ = strings.TrimSpace(g.Greet("world"))
}
`)

	return dir
}

// The maps the resolver reads must survive, and the ones nothing reads must be
// released. Pinning the KEPT set is the point: a dropped map reads as empty
// rather than panicking, so a future caller that starts reading Types would get
// a silent wrong answer, and only this test would notice.
//
// Mutation that must turn it RED: delete the releaseUnreadTypeInfo(pkgs) call
// from LoadPackages.
func TestLoadPackages_ReleasesUnreadTypeInfoMaps(t *testing.T) {
	dir := makeTwoPackageModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected at least one package")
	}

	pkg := result.Packages[0]
	if pkg.TypesInfo == nil {
		t.Fatal("expected TypesInfo to be populated")
	}

	// Kept — the resolver reads these.
	if len(pkg.TypesInfo.Defs) == 0 {
		t.Error("Defs must survive: the resolver reads it")
	}
	if len(pkg.TypesInfo.Uses) == 0 {
		t.Error("Uses must survive: the resolver reads it")
	}
	if len(pkg.TypesInfo.Selections) == 0 {
		t.Error("Selections must survive: the resolver reads it (resolver.go, " +
			"resolver_dispatch.go). Dropping it would not panic — a nil map reads " +
			"as empty — so method-call resolution would quietly return nothing.")
	}

	// Released — nothing reads these, and Types is the expensive one.
	if pkg.TypesInfo.Types != nil {
		t.Errorf("Types must be released, got %d entries", len(pkg.TypesInfo.Types))
	}
	if pkg.TypesInfo.Scopes != nil {
		t.Errorf("Scopes must be released, got %d entries", len(pkg.TypesInfo.Scopes))
	}
	if pkg.TypesInfo.Implicits != nil {
		t.Errorf("Implicits must be released, got %d entries", len(pkg.TypesInfo.Implicits))
	}
	if pkg.TypesInfo.Instances != nil {
		t.Errorf("Instances must be released, got %d entries", len(pkg.TypesInfo.Instances))
	}
	if pkg.TypesInfo.InitOrder != nil {
		t.Errorf("InitOrder must be released, got %d entries", len(pkg.TypesInfo.InitOrder))
	}
	if pkg.TypesInfo.FileVersions != nil {
		t.Errorf("FileVersions must be released, got %d entries", len(pkg.TypesInfo.FileVersions))
	}
}

// A dependency type-checked from source (SourceDeps) must be reduced to its
// *types.Package: its ASTs and per-expression info are most of the arena, and
// the resolver only ever reads the roots'.
//
// "strings" is a STDLIB dependency: the "./..." pattern never makes it a root,
// which is what separates a graph walk from a roots-only loop (the module's own
// "greet" package IS a root and would be cleaned by either).
//
// Mutation that must turn it RED: in releaseUnreadTypeInfo
// (internal/goanalysis/loader.go) delete the `p.Syntax = nil; p.TypesInfo = nil`
// branch for non-roots.
func TestLoadPackages_SourceDepsReleaseNonRootArena(t *testing.T) {
	dir := makeTwoPackageModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{SourceDeps: true})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var dep *packages.Package
	for _, root := range result.Packages {
		if imp, ok := root.Imports["strings"]; ok {
			dep = imp
		}
	}
	if dep == nil {
		t.Fatal("fixture is inert: the stdlib dependency was not loaded")
	}
	if dep.Types == nil || !dep.Types.Complete() {
		t.Fatal("the dependency's *types.Package must survive: roots import it")
	}
	if dep.Syntax != nil {
		t.Errorf("a non-root dependency's Syntax must be released, got %d files", len(dep.Syntax))
	}
	if dep.TypesInfo != nil {
		t.Error("a non-root dependency's TypesInfo must be released")
	}
	// The roots keep what the resolver reads.
	if len(result.Packages) == 0 || result.Packages[0].Syntax == nil || result.Packages[0].TypesInfo == nil {
		t.Error("a root must keep Syntax and TypesInfo")
	}
}

// By default dependencies come from export data: they are never type-checked
// from source, so no ASTs or type info exist to release, and the roots still
// type-check identically.
//
// Mutation that must turn it RED: in loadPackages (loader.go) make the mode
// unconditionally include packages.NeedDeps.
func TestLoadPackages_DefaultReadsDepsFromExportData(t *testing.T) {
	dir := makeTwoPackageModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var dep *packages.Package
	for _, root := range result.Packages {
		if imp, ok := root.Imports["strings"]; ok {
			dep = imp
		}
	}
	if dep == nil || dep.Types == nil {
		t.Fatal("fixture is inert: the stdlib dependency has no types")
	}
	if dep.ExportFile == "" {
		t.Error("default load must read dependencies from compiler export data (ExportFile empty: the dependency was type-checked from source)")
	}
	if dep.Syntax != nil || dep.TypesInfo != nil {
		t.Errorf("default load type-checked a dependency from source (Syntax=%d, TypesInfo=%v)",
			len(dep.Syntax), dep.TypesInfo != nil)
	}
	if len(goanalysis.Resolve(result.Packages)) == 0 {
		t.Error("roots must still resolve typed edges against export-data dependencies")
	}
}
