package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A package with an interface, one production implementation, one test-only
// fake, an in-package test and an external (_test package) test: the shape that
// makes go/packages produce "lib", "lib [lib.test]" and "lib_test [lib.test]".
var variantFixture = map[string]string{
	"go.mod": "module example.com/fx\n\ngo 1.22\n",
	"lib/lib.go": `package lib

type I interface{ M() }

type T struct{}

func (T) M() {}

func Use(i I) { i.M() }
`,
	"lib/ext.go": `package lib

import "net/http"

type H struct{}

func (H) ServeHTTP(http.ResponseWriter, *http.Request) {}

type E struct{}

func (E) Error() string { return "" }

func CallH(h http.Handler) { h.ServeHTTP(nil, nil) }

func CallE(e error) { _ = e.Error() }
`,
	"lib/fake_test.go": `package lib

type fake struct{}

func (fake) M() {}

func (fake) Error() string { return "" }
`,
	"lib/lib_test.go": `package lib

import "testing"

func TestUse(t *testing.T) {
	var i I = fake{}
	i.M()
	Use(fake{})
}
`,
	"lib/lib_x_test.go": `package lib_test

import (
	"testing"

	"example.com/fx/lib"
)

func TestExternal(t *testing.T) { lib.Use(lib.T{}) }
`,
}

func loadVariants(t *testing.T) (*goanalysis.LoadResult, []goanalysis.TypedEdge) {
	t.Helper()
	lr, err := goanalysis.LoadPackages(context.Background(), writeFixture(t, variantFixture), goanalysis.LoadOpts{Tests: true})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	return lr, goanalysis.ResolveWithTests(lr.Packages, lr.TestPackages)
}

func isTestFile(p string) bool { return strings.HasSuffix(p, "_test.go") }

// Test variants are held apart from Packages: folding them in would re-walk
// every production file and double IMPLEMENTS satisfaction.
func TestLoadPackages_TestVariantsAreSeparate(t *testing.T) {
	lr, _ := loadVariants(t)
	if len(lr.TestPackages) == 0 {
		t.Fatal("Tests:true produced no TestPackages")
	}
	for _, p := range lr.Packages {
		if p.ForTest != "" {
			t.Errorf("test variant %q leaked into Packages", p.ID)
		}
		if strings.HasSuffix(p.PkgPath, ".test") {
			t.Errorf("synthetic test main %q leaked into Packages", p.ID)
		}
	}
	if len(lr.Packages) != 1 { // example.com/fx/lib and nothing else
		t.Errorf("Packages = %d, want 1 (lib)", len(lr.Packages))
	}
	var sats int
	for _, s := range goanalysis.ComputeSatisfactions(lr.Packages) {
		if s.Type == "T" && s.Interface == "I" {
			sats++
		}
	}
	if sats != 1 {
		t.Errorf("T satisfies I %d times in IMPLEMENTS, want 1", sats)
	}
}

// With tests loaded there must be no duplicate call edges and no production
// function that dispatches to a test-only type.
func TestResolveWithTests_NoDuplicatesAndNoProdToTestEdges(t *testing.T) {
	_, edges := loadVariants(t)

	type key struct {
		callerFile, caller, calleeFile string
		callerLine, calleeLine, line   uint32
	}
	seen := map[key]int{}
	for _, e := range edges {
		seen[key{e.CallerFile, e.CallerName, e.CalleeFile, e.CallerLine, e.CalleeLine, e.Line}]++
		if !isTestFile(e.CallerFile) && isTestFile(e.CalleeFile) {
			t.Errorf("production %s -> test-file callee %s (%s:%d)", e.CallerName, e.CalleeName, e.CalleeFile, e.CalleeLine)
		}
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("edge emitted %d times: %+v", n, k)
		}
	}

	var useToT int
	for _, e := range edges {
		if e.CallerName == "Use" && e.CalleeName == "M" && strings.HasSuffix(e.CalleeFile, "lib.go") {
			useToT++
		}
	}
	if useToT != 1 {
		t.Errorf("Use -> T.M emitted %d times, want 1", useToT)
	}
}

// A test file's own interface dispatch must still see the test-only fake (and
// the production type), each exactly once.
func TestResolveWithTests_TestDispatchSeesFakeOnce(t *testing.T) {
	_, edges := loadVariants(t)
	var toFake, toT int
	for _, e := range edges {
		if e.CallerName != "TestUse" || e.CalleeName != "M" || !e.IsInterface {
			continue
		}
		if isTestFile(e.CalleeFile) {
			toFake++
		} else {
			toT++
		}
	}
	if toFake != 1 || toT != 1 {
		t.Errorf("TestUse i.M(): fake edges=%d (want 1), T edges=%d (want 1)", toFake, toT)
	}
}

// Every callee edge carries its declaration line, the second half of the
// callee's identity.
func TestResolve_CalleeLineIsDeclarationLine(t *testing.T) {
	_, edges := loadVariants(t)
	for _, e := range edges {
		if e.CallerName == "Use" && e.CalleeName == "M" && e.CalleeLine != 7 {
			t.Errorf("Use -> M CalleeLine = %d, want 7 (func (T) M() in lib.go)", e.CalleeLine)
		}
	}
}

// A call through an interface declared OUTSIDE the loaded packages (error,
// http.Handler, io.Writer ...) must still reach the repo types implementing it:
// implementers come from types.Implements, not from a table of in-repo
// interfaces. Production code must not reach test-only implementers.
func TestResolve_ExternalInterfaceDispatchReachesRepoImplementers(t *testing.T) {
	_, edges := loadVariants(t)
	find := func(caller, callee string) []goanalysis.TypedEdge {
		var out []goanalysis.TypedEdge
		for _, e := range edges {
			if e.CallerName == caller && e.CalleeName == callee {
				out = append(out, e)
			}
		}
		return out
	}
	for _, c := range []struct{ caller, callee, recv string }{
		{"CallH", "ServeHTTP", "H"},
		{"CallE", "Error", "E"},
	} {
		got := find(c.caller, c.callee)
		if len(got) != 1 || got[0].ReceiverType != c.recv || !strings.HasSuffix(got[0].CalleeFile, "ext.go") {
			t.Errorf("%s -> %s: want exactly one edge to %s in ext.go, got %+v", c.caller, c.callee, c.recv, got)
		}
	}
}

// Dispatch through an interface reaches the method each implementer really has,
// promoted ones included: two types embedding Base share Base.M (one edge, not
// two), a type shadowing it contributes its own, and a type that embeds two
// types both defining M is ambiguous, so it does not implement the interface
// and gets no edge.
//
// Mutation that must turn it RED: in implIndex.method (resolver.go) look the
// method up only among the type's declared methods instead of calling
// types.LookupFieldOrMethod.
func TestResolve_PromotedMethodDispatch(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"p/p.go": `package p

type I interface{ M() }

type Base struct{}

func (Base) M() {}

type A struct{ Base }

type B struct{ Base }

type C struct{ Base }

func (C) M() {}

type X struct{}

func (X) M() {}

type Y struct{}

func (Y) M() {}

type D struct {
	X
	Y
}

func UseM(i I) { i.M() }
`,
	})
	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	type decl struct {
		file string
		line uint32
	}
	got := map[decl]int{}
	for _, e := range goanalysis.Resolve(lr.Packages) {
		if e.CallerName == "UseM" && e.CalleeName == "M" {
			got[decl{filepath.Base(e.CalleeFile), e.CalleeLine}]++
		}
	}
	// Base.M line 7, C.M line 15, X.M line 19, Y.M line 23; D is ambiguous.
	want := map[decl]int{{"p.go", 7}: 1, {"p.go", 15}: 1, {"p.go", 19}: 1, {"p.go", 23}: 1}
	for d, n := range want {
		if got[d] != n {
			t.Errorf("UseM -> M at %s:%d: %d edges, want %d (got %v)", d.file, d.line, got[d], n, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("UseM reaches %d declarations, want %d: %v", len(got), len(want), got)
	}
}
