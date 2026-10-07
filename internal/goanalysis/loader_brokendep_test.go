package goanalysis_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

// A dependency outside the module's ./... that does not compile has no export
// data. go/packages type-checks such a package from source ("incompletely"),
// so its importers keep their typed edges. Losing that would silently drop every
// edge into an unbuildable dependency, so the upstream behaviour the export-data
// default relies on is pinned here.
//
// Mutation that must turn it RED: none of ours. A go/packages upgrade that stops
// falling back to source for a package without export data turns it RED.
func TestLoadPackages_BrokenExternalDepStillResolves(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"go.mod": `module example.com/fx

go 1.22

require example.com/dep v0.0.0

replace example.com/dep => ./third/dep
`,
		"third/dep/go.mod": "module example.com/dep\n\ngo 1.22\n",
		"third/dep/dep.go": "package dep\n\nfunc Ok() {}\n\nfunc Bad() int { return \"s\" }\n",
		"app/app.go":       "package app\n\nimport \"example.com/dep\"\n\nfunc A() { dep.Ok() }\n",
	})

	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, e := range goanalysis.Resolve(lr.Packages) {
		if e.CallerName == "A" && e.CalleeName == "Ok" {
			found = true
		}
	}
	if !found {
		t.Errorf("A -> dep.Ok missing: the broken dependency lost its types (errors: %v)", lr.Errors)
	}
}
