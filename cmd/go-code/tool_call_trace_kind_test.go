package main

import (
	"context"
	"encoding/xml"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestCallTrace_CallerKindsAndProductionCount(t *testing.T) {
	origTraceFromAGE := callTraceTraceFromAGE
	defer func() { callTraceTraceFromAGE = origTraceFromAGE }()

	target := &parser.Symbol{Name: "doWork", Kind: parser.KindFunction, File: "/repo/worker.go", StartLine: 50, EndLine: 80}
	prodCaller := &parser.Symbol{Name: "RunJob", Kind: parser.KindFunction, File: "/repo/job.go", StartLine: 10, EndLine: 40}
	testCaller := &parser.Symbol{Name: "TestDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 5, EndLine: 20}
	benchCaller := &parser.Symbol{Name: "BenchmarkDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 30, EndLine: 40}
	exampleCaller := &parser.Symbol{Name: "ExampleDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 50, EndLine: 60}
	helperCaller := &parser.Symbol{Name: "helper", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 70, EndLine: 80}

	result := &callgraph.TraceResult{
		Root:       target,
		TotalNodes: 6,
		MaxDepth:   1,
		Resolved:   5,
		Unresolved: 0,
		Tier:       "enhanced",
		Tree: []callgraph.CallChainNode{{
			Symbol:     target,
			CallerKind: "production",
			Children: []callgraph.CallChainNode{
				{Symbol: prodCaller, CallerKind: "production", CallLine: 25},
				{Symbol: testCaller, CallerKind: "test", CallLine: 12},
				{Symbol: benchCaller, CallerKind: "benchmark", CallLine: 35},
				{Symbol: exampleCaller, CallerKind: "example", CallLine: 55},
				{Symbol: helperCaller, CallerKind: "test", CallLine: 75},
			},
		}},
	}

	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return result, nil
	}

	root := t.TempDir()
	input := CallTraceInput{Repo: root, Symbol: "doWork", Direction: "callers", Depth: 2, Compact: true}
	res, err := handleCallTrace(context.Background(), input, analyze.Deps{}, nil, "", &codegraph.Store{})
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error response: %s", textContentOf(t, res))
	}

	text := textContentOf(t, res)
	var parsed xmlTraceResponse
	if err := xml.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse xml: %v\n%s", err, text)
	}

	if parsed.Trace.ProductionCallerCount != 1 {
		t.Errorf("production_caller_count = %d, want 1", parsed.Trace.ProductionCallerCount)
	}

	wantKinds := map[string]string{
		"RunJob":          "production",
		"TestDoWork":      "test",
		"BenchmarkDoWork": "benchmark",
		"ExampleDoWork":   "example",
		"helper":          "test",
	}

	if len(parsed.Trace.Nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(parsed.Trace.Nodes))
	}
	rootNode := parsed.Trace.Nodes[0]
	if rootNode.Kind != "function" {
		t.Errorf("root symbol kind = %q, want function", rootNode.Kind)
	}
	if rootNode.CallerKind != "production" {
		t.Errorf("root caller kind = %q, want production", rootNode.CallerKind)
	}
	if len(rootNode.Children) != 5 {
		t.Fatalf("expected 5 caller nodes, got %d", len(rootNode.Children))
	}
	for _, child := range rootNode.Children {
		want, ok := wantKinds[child.Name]
		if !ok {
			t.Errorf("unexpected caller %q", child.Name)
			continue
		}
		if child.Kind != "function" {
			t.Errorf("caller %q symbol kind = %q, want function", child.Name, child.Kind)
		}
		if child.CallerKind != want {
			t.Errorf("caller %q caller kind = %q, want %q", child.Name, child.CallerKind, want)
		}
	}
}

func TestCallTrace_ProductionCount_DirectDedup(t *testing.T) {
	origTraceFromAGE := callTraceTraceFromAGE
	defer func() { callTraceTraceFromAGE = origTraceFromAGE }()

	target := &parser.Symbol{Name: "doWork", Kind: parser.KindFunction, File: "/repo/worker.go", StartLine: 50, EndLine: 80}
	runJob := &parser.Symbol{Name: "RunJob", Kind: parser.KindFunction, File: "/repo/job.go", StartLine: 10, EndLine: 40}
	process := &parser.Symbol{Name: "Process", Kind: parser.KindFunction, File: "/repo/process.go", StartLine: 20, EndLine: 50}
	sharedUtil := &parser.Symbol{Name: "SharedUtil", Kind: parser.KindFunction, File: "/repo/util.go", StartLine: 30, EndLine: 60}
	testCaller := &parser.Symbol{Name: "TestDoWork", Kind: parser.KindFunction, File: "/repo/worker_test.go", StartLine: 100, EndLine: 120}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target, runJob, process, sharedUtil, testCaller},
		Edges: []callgraph.CallEdge{
			{Caller: runJob, Callee: target, CalleeName: "doWork", Line: 25},
			{Caller: process, Callee: target, CalleeName: "doWork", Line: 35},
			{Caller: sharedUtil, Callee: target, CalleeName: "doWork", Line: 45},
			{Caller: testCaller, Callee: target, CalleeName: "doWork", Line: 110},
			{Caller: runJob, Callee: sharedUtil, CalleeName: "SharedUtil", Line: 15},
			{Caller: process, Callee: sharedUtil, CalleeName: "SharedUtil", Line: 25},
			{Caller: runJob, Callee: process, CalleeName: "Process", Line: 18},
			{Caller: process, Callee: runJob, CalleeName: "RunJob", Line: 22},
			{Caller: sharedUtil, Callee: runJob, CalleeName: "RunJob", Line: 55},
		},
		Tier: "enhanced",
	}

	traceResult := callgraph.Trace(context.Background(), cg, "doWork", callgraph.TraceOpts{Direction: "callers", MaxDepth: 10})

	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return &traceResult, nil
	}

	root := t.TempDir()
	input := CallTraceInput{Repo: root, Symbol: "doWork", Direction: "callers", Depth: 10, Compact: true}
	res, err := handleCallTrace(context.Background(), input, analyze.Deps{}, nil, "", &codegraph.Store{})
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error response: %s", textContentOf(t, res))
	}

	text := textContentOf(t, res)
	var parsed xmlTraceResponse
	if err := xml.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse xml: %v\n%s", err, text)
	}

	const wantProduction = 3
	if parsed.Trace.ProductionCallerCount != wantProduction {
		t.Errorf("production_caller_count = %d, want %d", parsed.Trace.ProductionCallerCount, wantProduction)
	}

	underResult := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{IncludeCallers: true})
	if underResult.ProductionCallerCount != wantProduction {
		t.Errorf("understand ProductionCallerCount = %d, want %d", underResult.ProductionCallerCount, wantProduction)
	}

	transitive := transitiveProductionCount(traceResult.Tree, true)
	if transitive <= wantProduction {
		t.Errorf("transitive production count = %d, must be greater than direct count %d for this graph", transitive, wantProduction)
	}
}

// transitiveProductionCount counts every production CallerKind node in the tree
// (excluding the root when skipRoot is true), without deduplication. This is the
// old/transitive behavior that the new direct-only count must not match.
func transitiveProductionCount(nodes []callgraph.CallChainNode, skipRoot bool) int {
	var count int
	var walk func([]callgraph.CallChainNode, int)
	walk = func(ns []callgraph.CallChainNode, depth int) {
		for _, n := range ns {
			if !(skipRoot && depth == 0) && n.CallerKind == "production" {
				count++
			}
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return count
}
