package callgraph

import (
	"context"
	"os"
	"path/filepath"
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
