package compound_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestUnderstand_CallerKinds(t *testing.T) {
	t.Parallel()
	target := makeFunc("doWork", "worker.go", 50, 80)
	prodCaller := makeFunc("RunJob", "job.go", 10, 40)
	testCaller := makeFunc("TestDoWork", "worker_test.go", 5, 20)
	benchCaller := makeFunc("BenchmarkDoWork", "worker_test.go", 30, 40)
	exampleCaller := makeFunc("ExampleDoWork", "worker_test.go", 50, 60)
	helperCaller := makeFunc("helper", "worker_test.go", 70, 80)

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target, prodCaller, testCaller, benchCaller, exampleCaller, helperCaller},
		Edges: []callgraph.CallEdge{
			{Caller: prodCaller, Callee: target, CalleeName: "doWork", Line: 25},
			{Caller: testCaller, Callee: target, CalleeName: "doWork", Line: 12},
			{Caller: benchCaller, Callee: target, CalleeName: "doWork", Line: 35},
			{Caller: exampleCaller, Callee: target, CalleeName: "doWork", Line: 55},
			{Caller: helperCaller, Callee: target, CalleeName: "doWork", Line: 75},
		},
		Tier: "enhanced",
	}

	result := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{
		IncludeCallers: true,
	})

	if len(result.Callers) != 5 {
		t.Fatalf("expected 5 callers, got %d", len(result.Callers))
	}

	wantKinds := map[string]string{
		"RunJob":          "production",
		"TestDoWork":      "test",
		"BenchmarkDoWork": "benchmark",
		"ExampleDoWork":   "example",
		"helper":          "test",
	}
	gotProduction := 0
	for _, c := range result.Callers {
		want, ok := wantKinds[c.Name]
		if !ok {
			t.Errorf("unexpected caller %q", c.Name)
			continue
		}
		if c.CallerKind != want {
			t.Errorf("caller %q caller kind = %q, want %q", c.Name, c.CallerKind, want)
		}
		if c.CallerKind == "production" {
			gotProduction++
		}
	}
	if result.ProductionCallerCount != gotProduction {
		t.Errorf("ProductionCallerCount = %d, want %d", result.ProductionCallerCount, gotProduction)
	}
}
