package callgraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/langutil"
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

func TestTrace_UnresolvedCallerKind(t *testing.T) {
	t.Parallel()
	target := &parser.Symbol{Name: "doWork", Kind: parser.KindFunction, File: "/repo/worker.go", StartLine: 50, EndLine: 80}
	prodCaller := &parser.Symbol{Name: "RunJob", Kind: parser.KindFunction, File: "/repo/job.go", StartLine: 10, EndLine: 40}

	g := &CallGraph{
		Symbols: []*parser.Symbol{target, prodCaller},
		Edges: []CallEdge{
			{Caller: prodCaller, Callee: target, CalleeName: "doWork", Line: 25},
			// Unresolved external caller: no caller symbol, only the callee is known.
			{Caller: nil, Callee: target, CalleeName: "ExternalCaller", Line: 99},
		},
	}

	result := Trace(context.Background(), g, "doWork", TraceOpts{Direction: "callers", MaxDepth: 2})

	if result.Unresolved != 1 {
		t.Fatalf("expected 1 unresolved, got %d", result.Unresolved)
	}

	root := result.Tree[0]
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 caller nodes, got %d", len(root.Children))
	}

	var unresolvedNode *CallChainNode
	for i := range root.Children {
		if root.Children[i].Symbol != nil && root.Children[i].Symbol.Kind == "external" {
			unresolvedNode = &root.Children[i]
		}
	}
	if unresolvedNode == nil {
		t.Fatalf("expected unresolved child node in tree")
	}
	if unresolvedNode.CallerKind != langutil.CallerKindUnresolved {
		t.Errorf("unresolved caller kind = %q, want %q", unresolvedNode.CallerKind, langutil.CallerKindUnresolved)
	}

	productionCount := 0
	for _, child := range root.Children {
		if child.CallerKind == langutil.CallerKindProduction {
			productionCount++
		}
	}
	if productionCount != 1 {
		t.Errorf("production caller count = %d, want 1 (unresolved must not count)", productionCount)
	}
}
