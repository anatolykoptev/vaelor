package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeGoFixture materialises files (relative path -> source) under a fresh
// temp dir. withModule adds a go.mod so BuildFromRepo runs the go/types pass
// (enhanced tier); without it the graph stays on the tree-sitter tier.
func writeGoFixture(t *testing.T, withModule bool, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if withModule {
		files["go.mod"] = "module example.com/scoped\n\ngo 1.22\n"
	}
	for rel, src := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// edgesFrom returns "callee@dir" strings for every RESOLVED edge out of caller.
func resolvedCallees(t *testing.T, cg *CallGraph, caller string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, e := range cg.Edges {
		if e.Caller == nil || e.Caller.Name != caller || e.Callee == nil {
			continue
		}
		out[e.Callee.Name+"@"+filepath.Base(filepath.Dir(e.Callee.File))] = true
	}
	return out
}

// TestBuildFromRepo_GoReferenceScoping pins Go's scoping rules for the
// heuristic argument-position / struct-literal-value captures (@call.argref,
// @call.argref_method), which the call graph resolves by bare name:
//
//   - a bare identifier is never a method (Go has no implicit receiver), and
//     never names a symbol outside its own package;
//   - a value selector (`p.Run`) never denotes a package-level function of the
//     SAME package, and a cross-package function is only reached through a
//     qualifier that names that package.
//
// Before the fix a local variable `f` passed as an argument bound to the one
// repo method named `f`, and `Run: p.Run` (a field read) bound to the one repo
// function `Run` — both wrong edges that the enhanced tier kept (MergeCallGraphs
// appends tree-sitter edges the typed pass did not produce).
func TestBuildFromRepo_GoReferenceScoping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		withModule bool
		wantTier   string
	}{
		{"tree-sitter tier", false, "basic"},
		{"enhanced tier keeps unmatched tree-sitter edges", true, "enhanced"},
	} {
		t.Run(tc.name, func(t *testing.T) { checkGoReferenceScoping(t, tc.withModule, tc.wantTier) })
	}
}

func checkGoReferenceScoping(t *testing.T, withModule bool, wantTier string) {
	t.Helper()
	dir := writeGoFixture(t, withModule, map[string]string{
		// Caller: `f` and `p` are locals/params; Register's args are the
		// heuristic captures.
		"app/app.go": `package app

import "example.com/scoped/ext"

type Item struct{ Name string }

type Source struct{ Run func() }

func Process(f *Item, p Source) {
	Consume(f)
	_ = Plan{Run: p.Run}
	Register("hook", handler)
	Register("ext", ext.Handle)
}

type Plan struct{ Run func() }

func handler() {}

func Consume(*Item) {}

func Register(string, func()) {}
`,
		// The ONLY method named f in the repo, in another package.
		"printer/printer.go": `package printer

type Printer struct{}

func (p Printer) f(format string) {}
`,
		// The ONLY function named Run in the repo, in another package.
		"runner/runner.go": `package runner

func Run() {}
`,
		// A genuinely package-qualified function reference.
		"ext/ext.go": `package ext

func Handle() {}
`,
	})

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, Language: "go"})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if cg.Tier != wantTier {
		t.Fatalf("tier = %q, want %q (fixture does not exercise the intended leg)", cg.Tier, wantTier)
	}
	got := resolvedCallees(t, cg, "Process")

	// Wrong edges: local var `f` -> method f; field read `p.Run` -> func Run.
	if got["f@printer"] {
		t.Errorf("Process -> printer.f: a bare local identifier bound to a method of another package; edges=%v", got)
	}
	if got["Run@runner"] {
		t.Errorf("Process -> runner.Run: a value selector p.Run bound to a function it cannot name; edges=%v", got)
	}

	// Real references must survive, or dead_code would flag live handlers.
	if !got["handler@app"] {
		t.Errorf("Process -> app.handler (same-package function reference) was dropped; edges=%v", got)
	}
	if !got["Handle@ext"] {
		t.Errorf("Process -> ext.Handle (package-qualified function reference) was dropped; edges=%v", got)
	}
	if !got["Consume@app"] {
		t.Errorf("Process -> app.Consume (plain call) was dropped; edges=%v", got)
	}
}

// TestBuildFromRepo_GoBareCallNeverMethod pins the call (not reference) side:
// `f(x)` where f is a local func value must not bind to a method f, even when
// that method sits in the caller's own package — a bare identifier cannot name
// a method.
func TestBuildFromRepo_GoBareCallNeverMethod(t *testing.T) {
	dir := writeGoFixture(t, false, map[string]string{
		"pkg/a.go": `package pkg

type W struct{}

func (w W) f(s string) {}

func Use(f func(string)) {
	f("x")
}
`,
	})
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, Language: "go"})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if got := resolvedCallees(t, cg, "Use"); got["f@pkg"] {
		t.Errorf("Use -> W.f: bare call of a local func value bound to a method; edges=%v", got)
	}
}

// TestBuildFromRepo_GoValueSelectorNeverSamePackageFunc: `p.Sync` where Sync is
// a package-level function of the caller's own package is a field or method
// selector on the value p — a package never qualifies its own names.
func TestBuildFromRepo_GoValueSelectorNeverSamePackageFunc(t *testing.T) {
	dir := writeGoFixture(t, false, map[string]string{
		"pkg/a.go": `package pkg

type Hooks struct{ Sync func() }

func Sync() {}

func Wire(p Hooks) Hooks {
	return Hooks{Sync: p.Sync}
}
`,
	})
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, Language: "go"})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if got := resolvedCallees(t, cg, "Wire"); got["Sync@pkg"] {
		t.Errorf("Wire -> pkg.Sync: value selector p.Sync bound to the caller's own package function; edges=%v", got)
	}
}

// tiers runs fn on the tree-sitter tier (no go.mod) and the enhanced tier
// (go.mod present, so the typed pass runs and tree-sitter edges are merged in).
func tiers(t *testing.T, fn func(t *testing.T, withModule bool, wantTier string)) {
	t.Helper()
	for _, tc := range []struct {
		name       string
		withModule bool
		wantTier   string
	}{
		{"tree-sitter tier", false, "basic"},
		{"enhanced tier", true, "enhanced"},
	} {
		t.Run(tc.name, func(t *testing.T) { fn(t, tc.withModule, tc.wantTier) })
	}
}

func buildTier(t *testing.T, dir, wantTier string) *CallGraph {
	t.Helper()
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, Language: "go"})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}
	if cg.Tier != wantTier {
		t.Fatalf("tier = %q, want %q (fixture does not exercise the intended leg)", cg.Tier, wantTier)
	}
	return cg
}

// A function referenced ONLY as a value (`Register("k", q.Handle)`) must keep
// its edge however the qualifier is spelled: the typed pass emits no
// value-reference edges, so dropping it makes the function look dead. The
// qualifier is whatever the referencing file's import table says -- an alias,
// or the imported package's `package` clause name -- never the directory base
// (`foo-go` declares `package foo`; `vx/v2` declares `package vx`).
func TestBuildFromRepo_GoQualifiedReferenceSpellings(t *testing.T) {
	tiers(t, func(t *testing.T, withModule bool, wantTier string) {
		dir := writeGoFixture(t, withModule, map[string]string{
			"app/app.go": `package app

import (
	"strconv"
	"strings"
	ext2 "example.com/scoped/ext"
	"example.com/scoped/foo-go"
	"example.com/scoped/vx/v2"
)

func Register(string, func()) {}

func RefAlias()   { Register("a", ext2.HandleAlias) }
func RefDirName() { Register("b", foo.HandleFoo) }
func RefV2()      { Register("c", vx.HandleV2) }
`,
			"ext/ext.go":    "package ext\n\nfunc HandleAlias() {}\n",
			"foo-go/foo.go": "package foo\n\nfunc HandleFoo() {}\n",
			"vx/v2/vx.go":   "package vx\n\nfunc HandleV2() {}\n",
		})
		cg := buildTier(t, dir, wantTier)
		for caller, want := range map[string]string{
			"RefAlias":   "HandleAlias@ext",
			"RefDirName": "HandleFoo@foo-go",
			"RefV2":      "HandleV2@v2",
		} {
			if got := resolvedCallees(t, cg, caller); !got[want] {
				t.Errorf("%s -> %s: value reference through an import spelling the directory base does not match was dropped; edges=%v", caller, want, got)
			}
		}
	})
}

// With the import table the qualifier picks the package even when the function
// name is not unique repo-wide.
func TestBuildFromRepo_GoQualifiedReferencePicksImportedPackage(t *testing.T) {
	tiers(t, func(t *testing.T, withModule bool, wantTier string) {
		dir := writeGoFixture(t, withModule, map[string]string{
			"app/app.go": `package app

import other "example.com/scoped/b"

func Register(string, func()) {}

func Wire() { Register("a", other.Handle) }
`,
			"a/a.go": "package a\n\nfunc Handle() {}\n",
			"b/b.go": "package b\n\nfunc Handle() {}\n",
		})
		cg := buildTier(t, dir, wantTier)
		got := resolvedCallees(t, cg, "Wire")
		if !got["Handle@b"] || got["Handle@a"] {
			t.Errorf("Wire must reference b.Handle only; edges=%v", got)
		}
	})
}

// An external test package (`package x_test`, same dir) reaches x only through
// `x.Foo`. The target sits in the caller's directory but in another package.
func TestBuildFromRepo_GoExternalTestPackageQualifiedCall(t *testing.T) {
	tiers(t, func(t *testing.T, withModule bool, wantTier string) {
		dir := writeGoFixture(t, withModule, map[string]string{
			"ext/ext.go": "package ext\n\nfunc OnlyTested() {}\n",
			"ext/ext_test.go": `package ext_test

import (
	"strconv"
	"strings"
	"testing"

	"example.com/scoped/ext"
)

func TestX(t *testing.T) { ext.OnlyTested() }
`,
			// Same name elsewhere: the repo-wide unique-name tier cannot decide.
			"other/other.go": "package other\n\nfunc OnlyTested() {}\n",
		})
		cg := buildTier(t, dir, wantTier)
		got := resolvedCallees(t, cg, "TestX")
		if !got["OnlyTested@ext"] || got["OnlyTested@other"] {
			t.Errorf("TestX -> ext.OnlyTested lost (or bound to the wrong package); edges=%v", got)
		}
	})
}

// `import . "pkg"` puts pkg's exported names in file scope: a bare identifier
// may then name a function of another package.
func TestBuildFromRepo_GoDotImport(t *testing.T) {
	tiers(t, func(t *testing.T, withModule bool, wantTier string) {
		dir := writeGoFixture(t, withModule, map[string]string{
			"app/app.go": `package app

import . "example.com/scoped/dot"

func Register(string, func()) {}

func CallDot() { DotFn() }
func RefDot()  { Register("d", DotFn) }
`,
			"dot/dot.go": "package dot\n\nfunc DotFn() {}\n",
			// A same-named function in a package that is NOT dot-imported.
			"elsewhere/e.go": "package elsewhere\n\nfunc DotFn() {}\n",
		})
		cg := buildTier(t, dir, wantTier)
		for _, caller := range []string{"CallDot", "RefDot"} {
			got := resolvedCallees(t, cg, caller)
			if !got["DotFn@dot"] || got["DotFn@elsewhere"] {
				t.Errorf("%s -> dot.DotFn lost (or bound to a non-imported package); edges=%v", caller, got)
			}
		}
	})
}

// Shared L2 entries built by an older resolver must not be read back by this
// binary: the resolver version is part of the key prefix.
func TestCGL2PrefixCarriesResolverVersion(t *testing.T) {
	want := "r" + strconv.Itoa(ResolverVersion) + ":"
	if !strings.HasSuffix(cgL2Prefix, want) {
		t.Errorf("cgL2Prefix = %q, want suffix %q", cgL2Prefix, want)
	}
}

// A method called through a value in an external test package targets the
// package under test (same directory), even when other packages declare a
// method with the same name: without types, the pair x / x_test is one family.
func TestBuildFromRepo_GoExternalTestPackageMethodCall(t *testing.T) {
	tiers(t, func(t *testing.T, withModule bool, wantTier string) {
		dir := writeGoFixture(t, withModule, map[string]string{
			"ext/ext.go": "package ext\n\ntype Reg struct{}\n\nfunc (r *Reg) Register() {}\n",
			"ext/ext_test.go": `package ext_test

import (
	"testing"

	"example.com/scoped/ext"
)

func TestY(t *testing.T) {
	r := &ext.Reg{}
	r.Register()
}
`,
			"other/other.go": "package other\n\ntype Reg struct{}\n\nfunc (r *Reg) Register() {}\n",
		})
		cg := buildTier(t, dir, wantTier)
		got := resolvedCallees(t, cg, "TestY")
		if !got["Register@ext"] || got["Register@other"] {
			t.Errorf("TestY -> ext.Reg.Register lost (or bound to the wrong package); edges=%v", got)
		}
	})
}
