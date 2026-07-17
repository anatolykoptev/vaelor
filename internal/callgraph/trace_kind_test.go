package callgraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestTrace_CallerKinds(t *testing.T) {
	t.Parallel()
	target := &parser.Symbol{Name: "doWork", Kind: parser.KindFunction, File: "/repo/worker.go", StartLine: 50, EndLine: 80}
	prodCaller := &parser.Symbol{Name: "RunJob", Kind: parser.KindFunction, File: "/repo/job.go", StartLine: 10, EndLine: 40}
	testCaller := &parser.Symbol{Name: "TestDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 5, EndLine: 20}
	benchCaller := &parser.Symbol{Name: "BenchmarkDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 30, EndLine: 40}
	exampleCaller := &parser.Symbol{Name: "ExampleDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 50, EndLine: 60}
	helperCaller := &parser.Symbol{Name: "helper", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 70, EndLine: 80}

	g := &CallGraph{
		Symbols: []*parser.Symbol{target, prodCaller, testCaller, benchCaller, exampleCaller, helperCaller},
		Edges: []CallEdge{
			{Caller: prodCaller, Callee: target, CalleeName: "doWork", Line: 25},
			{Caller: testCaller, Callee: target, CalleeName: "doWork", Line: 12},
			{Caller: benchCaller, Callee: target, CalleeName: "doWork", Line: 35},
			{Caller: exampleCaller, Callee: target, CalleeName: "doWork", Line: 55},
			{Caller: helperCaller, Callee: target, CalleeName: "doWork", Line: 75},
		},
	}

	result := Trace(context.Background(), g, "doWork", TraceOpts{Direction: "callers", MaxDepth: 2})

	if len(result.Tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(result.Tree))
	}
	root := result.Tree[0]
	if root.CallerKind != "production" {
		t.Errorf("root caller kind = %q, want production", root.CallerKind)
	}
	if len(root.Children) != 5 {
		t.Fatalf("expected 5 callers, got %d", len(root.Children))
	}

	wantKinds := map[string]string{
		"RunJob":          "production",
		"TestDoWork":      "test",
		"BenchmarkDoWork": "benchmark",
		"ExampleDoWork":   "example",
		"helper":          "test",
	}
	for _, child := range root.Children {
		want, ok := wantKinds[child.Symbol.Name]
		if !ok {
			t.Errorf("unexpected caller %q", child.Symbol.Name)
			continue
		}
		if child.CallerKind != want {
			t.Errorf("caller %q kind = %q, want %q", child.Symbol.Name, child.CallerKind, want)
		}
	}
}
