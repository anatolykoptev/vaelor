package main

import (
	"context"
	"encoding/xml"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/codegraph"
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
