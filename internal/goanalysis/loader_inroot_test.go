package goanalysis_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

func inRootFixture(cdep string) map[string]string {
	return map[string]string{
		"go.mod": `module example.com/fx

go 1.22

require example.com/cdep v0.0.0

replace example.com/cdep => ./libs/cdep
`,
		"libs/cdep/go.mod":  "module example.com/cdep\n\ngo 1.22\n",
		"libs/cdep/cdep.go": cdep,
		"app/app.go": `package app

import "example.com/cdep"

func UseParser(p *cdep.Parser) { p.Close() }

func UseTree(t *cdep.Tree) { t.Close() }
`,
	}
}

const plainCdep = `package cdep

type Parser struct{}

func (*Parser) Close() {}

type Tree struct{}

func (*Tree) Close() {
}
`

// A dependency that lives INSIDE the repository but is not matched by ./...
// (a `replace => ./libs/x` module) must be type-checked from source like a root:
// export data does not carry faithful declaration positions for cgo packages, and
// a callee is identified by (name, file, line).
//
// Mutation that must turn it RED: in LoadPackages (loader.go) delete the
// `inRootDependencies` reload block.
func TestLoadPackages_InRepoDependencyIsLoadedFromSource(t *testing.T) {
	dir := writeFixture(t, inRootFixture(plainCdep))
	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range lr.Packages {
		if p.PkgPath == "example.com/cdep" {
			found = true
			if len(p.Syntax) == 0 || p.TypesInfo == nil {
				t.Error("the in-repo dependency must be a source-typechecked root (Syntax and TypesInfo present)")
			}
		}
	}
	if !found {
		t.Error("the in-repo dependency example.com/cdep was left as an export-data dependency")
	}
}

// The reviewer's scenario end to end: a cgo package inside the repo with two
// same-named methods in one file. Declared lines must be the SOURCE lines, so
// each caller binds to its own Close — before, export data shifted every line and
// a one-line Parser.Close followed by Tree.Close bound UseParser to Tree.Close.
//
// Skipped only when cgo cannot work on this machine (no C compiler).
func TestLoadPackages_InRepoCgoDependencyKeepsSourceLines(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		if _, err2 := exec.LookPath("clang"); err2 != nil {
			t.Skip("no C compiler: cgo cannot be exercised here")
		}
	}
	cgo := `package cdep

/*
#include <stdlib.h>
*/
import "C"

type Parser struct{}

func (*Parser) Close() {}

type Tree struct{}

func (*Tree) Close() {
	_ = C.int(0)
}
`
	dir := writeFixture(t, inRootFixture(cgo))
	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]uint32{}
	for _, e := range goanalysis.Resolve(lr.Packages) {
		if (e.CallerName == "UseParser" || e.CallerName == "UseTree") && e.CalleeName == "Close" {
			if !strings.HasSuffix(e.CalleeFile, "cdep.go") {
				t.Errorf("%s -> Close resolved to %s", e.CallerName, e.CalleeFile)
			}
			lines[e.CallerName] = e.CalleeLine
		}
	}
	// Parser.Close is declared on line 10, Tree.Close on line 14 of the source.
	if lines["UseParser"] != 10 || lines["UseTree"] != 14 {
		t.Errorf("callee lines = %v, want UseParser:10 UseTree:14 (source positions)", lines)
	}
}

// Under -mod=vendor a vendored dependency lives inside the repository too, but it
// is never ingested and must NOT be reloaded as a root: that would type-check
// every vendored dependency from source (the memory shape this package exists to
// avoid).
//
// Mutation that must turn it RED: in inRootDependencies (loader.go) delete the
// `!strings.HasPrefix(f, vendor)` condition.
func TestLoadPackages_VendoredDependencyStaysExportData(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"go.mod":                    "module example.com/fx\n\ngo 1.22\n\nrequire example.com/v v1.0.0\n",
		"vendor/modules.txt":        "# example.com/v v1.0.0\n## explicit; go 1.22\nexample.com/v\n",
		"vendor/example.com/v/v.go": "package v\n\nfunc V() {}\n",
		"app/app.go":                "package app\n\nimport \"example.com/v\"\n\nfunc A() { v.V() }\n",
	})
	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var app bool
	for _, p := range lr.Packages {
		if p.PkgPath == "example.com/v" {
			t.Error("a vendored dependency was reloaded as a source-typechecked root")
		}
		app = app || p.PkgPath == "example.com/fx/app"
	}
	if !app {
		t.Fatalf("fixture is inert: the app package did not load (errors: %v)", lr.Errors)
	}
}
