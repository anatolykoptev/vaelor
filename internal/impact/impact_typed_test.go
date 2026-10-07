package impact_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/impact"
)

// writeModule materialises files (relative path -> content) under a temp dir.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// typedImpact runs the real pipeline (ingest -> tree-sitter -> go/types merge)
// and impact.Analyze on target.
func typedImpact(t *testing.T, root, target string) *impact.Result {
	t.Helper()
	pr, err := callgraph.BuildAndEnrich(context.Background(), callgraph.PipelineOpts{
		Root:        root,
		TypedEnrich: true,
	})
	if err != nil {
		t.Fatalf("BuildAndEnrich: %v", err)
	}
	if pr.CG.Tier != "enhanced" {
		t.Fatalf("typed enrichment did not run (tier=%q); the test would not exercise the merge", pr.CG.Tier)
	}
	res := impact.Analyze(context.Background(), pr.CG, target, impact.Options{})
	if !res.Found {
		t.Fatalf("target %q not found", target)
	}
	return res
}

func allCallers(r *impact.Result) []impact.AffectedSymbol {
	return append(append([]impact.AffectedSymbol{}, r.DirectCallers...), r.TransitiveCallers...)
}

// Regression for #858. Two `package main` directories share a function name
// and a file basename; only cmd/z reaches lib.Target. cmd/a (sorting first, so
// a first-wins name lookup lands on it) merely calls
// flag.FlagSet.Parse, which name-matches the unrelated repo function lib.Parse
// (which does reach Target).
//
// Before the fix the typed->tree-sitter conversion keyed symbols by
// name+basename ("main:main.go"), so a typed edge from cmd/z's main was bound to
// whichever main came first (cmd/a's), and an external callee (fs.Parse) fell
// back to the first repo symbol named Parse. The wrong `main` (cmd/a) was reported as an
// affected caller and its package as affected.
func TestAnalyze_SameNamedMainsAcrossPackages_OnlyReachingOneIsReported(t *testing.T) {
	root := writeModule(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"lib/lib.go": `package lib

func Target() {}

type Svc struct{}

func (s *Svc) Sync() { Target() }
`,
		// An unrelated repo function that happens to share a name with a stdlib
		// method cmd/a calls, and that does reach Target.
		"lib/parse.go": `package lib

func Parse() { Target() }
`,
		"cmd/z/main.go": `package main

import "example.com/fx/lib"

func main() {
	s := &lib.Svc{}
	s.Sync()
}
`,
		"cmd/a/main.go": `package main

import "flag"

func main() {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	_ = fs.Parse(nil)
}
`,
	})

	res := typedImpact(t, root, "Target")

	var mains []impact.AffectedSymbol
	for _, c := range allCallers(res) {
		if c.Name == "main" {
			mains = append(mains, c)
		}
	}
	if len(mains) != 1 {
		t.Fatalf("want exactly one reaching main, got %d: %+v", len(mains), mains)
	}
	if got, want := mains[0].Package, filepath.Join(root, "cmd", "z"); got != want {
		t.Errorf("main attributed to %s, want %s", got, want)
	}
	for _, p := range res.AffectedPackages {
		if p == filepath.Join(root, "cmd", "a") {
			t.Errorf("cmd/a cannot reach Target but is listed in affected_packages: %v", res.AffectedPackages)
		}
	}
}

// Regression for #858: a test calling a method whose name is shared with a
// test-file fake must still be found as a transitive caller. Without type
// information for _test.go files the call `s.Sync()` resolves by name to the
// fake (first in the directory), and TestSync silently vanishes from the blast
// radius.
func TestAnalyze_TestCallingSharedMethodName_IsReachedTransitively(t *testing.T) {
	root := writeModule(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"lib/lib.go": `package lib

func Target() {}

type Svc struct{}

func (s *Svc) Sync() { Target() }
`,
		// Sorts before lib.go, so name-only resolution picks it first.
		"lib/afake_test.go": `package lib

type fake struct{}

func (fake) Sync() {}
`,
		"lib/lib_test.go": `package lib

import "testing"

func TestSync(t *testing.T) {
	s := &Svc{}
	s.Sync()
}
`,
	})

	res := typedImpact(t, root, "Target")

	var found *impact.AffectedSymbol
	for _, c := range allCallers(res) {
		if c.Name == "TestSync" {
			found = &c
		}
	}
	if found == nil {
		t.Fatalf("TestSync (calls Svc.Sync -> Target) missing from callers: %+v", allCallers(res))
	}
	if found.Distance != 2 {
		t.Errorf("TestSync distance = %d, want 2", found.Distance)
	}
}

// Same-named methods of two types in ONE file. OnlyB calls B.Do, which reaches
// TargetB; A.Do reaches TargetA. A callee resolved by name inside the file binds
// every `.Do()` to the first one: OnlyB then shows up under TargetA and is
// missing under TargetB.
func TestAnalyze_SameFileSameNameMethods_BothEndpointsResolvedByPosition(t *testing.T) {
	root := writeModule(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"lib/two.go": `package lib

func TargetA() {}
func TargetB() {}

type A struct{}

func (A) Do() { TargetA() }

type B struct{}

func (B) Do() { TargetB() }

func OnlyB(b B) { b.Do() }
`,
	})
	has := func(r *impact.Result, name string) bool {
		for _, c := range allCallers(r) {
			if c.Name == name {
				return true
			}
		}
		return false
	}
	if a := typedImpact(t, root, "TargetA"); has(a, "OnlyB") {
		t.Errorf("OnlyB calls B.Do, not A.Do, but is reported as a caller of TargetA: %+v", allCallers(a))
	}
	if b := typedImpact(t, root, "TargetB"); !has(b, "OnlyB") {
		t.Errorf("OnlyB -> B.Do -> TargetB missing: %+v", allCallers(b))
	}
}

// Calls through interfaces declared outside the module (error, http.Handler)
// must reach the repo types that implement them.
func TestAnalyze_CallThroughExternalInterface_ReachesImplementer(t *testing.T) {
	root := writeModule(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"lib/lib.go": `package lib

import "net/http"

func Target() {}

type H struct{}

func (H) ServeHTTP(http.ResponseWriter, *http.Request) { Target() }

type E struct{}

func (E) Error() string { Target(); return "" }

func Serve(h http.Handler) { h.ServeHTTP(nil, nil) }

func Fail(err error) { _ = err.Error() }
`,
	})
	res := typedImpact(t, root, "Target")
	got := map[string]int{}
	for _, c := range allCallers(res) {
		got[c.Name] = c.Distance
	}
	for _, name := range []string{"Serve", "Fail"} {
		if got[name] != 2 {
			t.Errorf("%s calls Target through an external interface (distance 2), got %v", name, got)
		}
	}
}

// A package-level function and a method with the same name share one file; each
// call form (bare ident, qualified pkg.F, method selection) must bind to its own
// declaration by position.
func TestAnalyze_SameFileFuncAndMethodSameName_ResolvedByPosition(t *testing.T) {
	root := writeModule(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"lib/lib.go": `package lib

func TargetF() {}
func TargetM() {}

type T struct{}

func (T) Do() { TargetM() }

func Do() { TargetF() }

func Local() { Do() }
`,
		"app/app.go": `package app

import "example.com/fx/lib"

func Qualified() { lib.Do() }

func Method(t lib.T) { t.Do() }
`,
	})
	has := func(r *impact.Result, name string) bool {
		for _, c := range allCallers(r) {
			if c.Name == name {
				return true
			}
		}
		return false
	}
	f, m := typedImpact(t, root, "TargetF"), typedImpact(t, root, "TargetM")
	for _, c := range []struct {
		r    *impact.Result
		name string
		want bool
	}{
		{f, "Local", true}, {f, "Qualified", true}, {f, "Method", false},
		{m, "Method", true}, {m, "Local", false}, {m, "Qualified", false},
	} {
		if has(c.r, c.name) != c.want {
			t.Errorf("%s as caller of %s = %v, want %v", c.name, c.r.Symbol, !c.want, c.want)
		}
	}
}
