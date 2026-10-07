package callgraph

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func fnSym(name, file string, start, end uint32) *parser.Symbol {
	return &parser.Symbol{Name: name, Kind: parser.KindMethod, File: file, StartLine: start, EndLine: end}
}

// Same-named methods of different types in ONE file: the callee must be chosen
// by declaration line, not by being first.
func TestConvertToCallGraph_SameFileSameNameCalleeByLine(t *testing.T) {
	const f = "/r/p/two.go"
	doA, doB := fnSym("Do", f, 5, 7), fnSym("Do", f, 10, 12)
	onlyB := fnSym("OnlyB", f, 15, 17)
	cg := ConvertToCallGraph([]goanalysis.TypedEdge{
		{CallerName: "OnlyB", CallerFile: f, CallerLine: 15, CalleeName: "Do", CalleeFile: f, CalleeLine: 10, Line: 16},
	}, []*parser.Symbol{doA, doB, onlyB})
	if len(cg.Edges) != 1 || cg.Edges[0].Callee != doB {
		t.Fatalf("OnlyB must call the Do declared at line 10, got %+v", cg.Edges)
	}
	// A line inside no candidate's span is unresolved, never the first symbol.
	cg = ConvertToCallGraph([]goanalysis.TypedEdge{
		{CallerName: "OnlyB", CallerFile: f, CallerLine: 15, CalleeName: "Do", CalleeFile: f, CalleeLine: 99, Line: 16},
	}, []*parser.Symbol{doA, doB, onlyB})
	if cg.Edges[0].Callee != nil {
		t.Fatalf("no span contains line 99, callee must be nil, got %v", cg.Edges[0].Callee)
	}
}

// Absolute callee paths that match no repo file (stdlib, module cache) stay
// unresolved even when a repo file shares the last path elements; relative
// (SCIP) paths resolve only when the suffix is unique.
func TestConvertToCallGraph_PathResolution(t *testing.T) {
	repo := fnSym("Parse", "/r/internal/flag/flag.go", 1, 9)
	caller := fnSym("main", "/r/cmd/main.go", 1, 9)
	syms := []*parser.Symbol{repo, caller}
	conv := func(calleeFile string) *parser.Symbol {
		cg := ConvertToCallGraph([]goanalysis.TypedEdge{
			{CallerName: "main", CallerFile: "/r/cmd/main.go", CalleeName: "Parse", CalleeFile: calleeFile},
		}, syms)
		return cg.Edges[0].Callee
	}
	if got := conv("/usr/local/go/src/internal/flag/flag.go"); got != nil {
		t.Errorf("GOROOT callee bound to repo symbol %v", got)
	}
	if got := conv("internal/flag/flag.go"); got != repo {
		t.Errorf("relative unique suffix must resolve, got %v", got)
	}
	other := fnSym("Parse", "/r/other/internal/flag/flag.go", 1, 9)
	cg := ConvertToCallGraph([]goanalysis.TypedEdge{
		{CallerName: "main", CallerFile: "/r/cmd/main.go", CalleeName: "Parse", CalleeFile: "internal/flag/flag.go"},
	}, append(syms, other))
	if cg.Edges[0].Callee != nil {
		t.Errorf("ambiguous relative suffix must be nil, got %v", cg.Edges[0].Callee)
	}
}

// The merge must not let a typed edge from one function suppress the
// tree-sitter edge of a different function with the same name: same name in
// different files, and same name + same file at different lines.
func TestMergeCallGraphs_SameNameCallersAreDistinct(t *testing.T) {
	typedCaller := fnSym("Do", "/r/a.go", 1, 5)
	for name, other := range map[string]*parser.Symbol{
		"other file":     fnSym("Do", "/r/b.go", 1, 5),
		"same file line": fnSym("Do", "/r/a.go", 10, 15),
	} {
		ts := &CallGraph{Edges: []CallEdge{{Caller: other, CalleeName: "X"}}}
		typed := &CallGraph{Edges: []CallEdge{{Caller: typedCaller, CalleeName: "X"}}}
		merged := MergeCallGraphs(ts, typed)
		if len(merged.Edges) != 2 {
			t.Errorf("%s: typed edge suppressed another function's edge; %d edges, want 2", name, len(merged.Edges))
		}
	}
	// Same function: the typed edge still wins over its tree-sitter twin.
	ts := &CallGraph{Edges: []CallEdge{{Caller: typedCaller, CalleeName: "X"}}}
	typed := &CallGraph{Edges: []CallEdge{{Caller: typedCaller, CalleeName: "X"}}}
	if n := len(MergeCallGraphs(ts, typed).Edges); n != 1 {
		t.Errorf("same caller+callee must dedupe to 1 edge, got %d", n)
	}
}
