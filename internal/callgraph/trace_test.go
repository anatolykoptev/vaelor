package callgraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestTrace_Callees(t *testing.T) {
	// Chain: main → serve → handle
	main := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10}
	serve := &parser.Symbol{Name: "serve", Kind: parser.KindFunction, File: "/src/server.go", StartLine: 1, EndLine: 20}
	handle := &parser.Symbol{Name: "handle", Kind: parser.KindFunction, File: "/src/handler.go", StartLine: 1, EndLine: 15}

	g := &CallGraph{
		Symbols: []*parser.Symbol{main, serve, handle},
		Edges: []CallEdge{
			{Caller: main, Callee: serve, CalleeName: "serve", Line: 5},
			{Caller: serve, Callee: handle, CalleeName: "handle", Line: 10},
		},
	}

	result := Trace(context.Background(), g, "main", TraceOpts{Direction: "callees"})

	if result.Root == nil || result.Root.Name != "main" {
		t.Fatalf("root should be main, got %v", result.Root)
	}
	if result.TotalNodes != 3 {
		t.Errorf("expected 3 total nodes (main, serve, handle), got %d", result.TotalNodes)
	}
	if result.MaxDepth != 2 {
		t.Errorf("expected max depth 2, got %d", result.MaxDepth)
	}
	if result.Resolved != 2 {
		t.Errorf("expected 2 resolved, got %d", result.Resolved)
	}

	if len(result.Tree) != 1 {
		t.Fatalf("expected 1 root node in tree, got %d", len(result.Tree))
	}
	rootNode := result.Tree[0]
	if len(rootNode.Children) != 1 || rootNode.Children[0].Symbol.Name != "serve" {
		t.Errorf("main should have one child 'serve', got %v", rootNode.Children)
	}
	serveNode := rootNode.Children[0]
	if len(serveNode.Children) != 1 || serveNode.Children[0].Symbol.Name != "handle" {
		t.Errorf("serve should have one child 'handle', got %v", serveNode.Children)
	}
}

func TestTrace_Callers(t *testing.T) {
	main := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10}
	serve := &parser.Symbol{Name: "serve", Kind: parser.KindFunction, File: "/src/server.go", StartLine: 1, EndLine: 20}
	handle := &parser.Symbol{Name: "handle", Kind: parser.KindFunction, File: "/src/handler.go", StartLine: 1, EndLine: 15}

	g := &CallGraph{
		Symbols: []*parser.Symbol{main, serve, handle},
		Edges: []CallEdge{
			{Caller: main, Callee: serve, CalleeName: "serve", Line: 5},
			{Caller: serve, Callee: handle, CalleeName: "handle", Line: 10},
		},
	}

	result := Trace(context.Background(), g, "handle", TraceOpts{Direction: "callers"})

	if result.Root == nil || result.Root.Name != "handle" {
		t.Fatalf("root should be handle, got %v", result.Root)
	}
	if result.TotalNodes != 3 {
		t.Errorf("expected 3 total nodes, got %d", result.TotalNodes)
	}
	rootNode := result.Tree[0]
	if len(rootNode.Children) != 1 || rootNode.Children[0].Symbol.Name != "serve" {
		t.Errorf("handle should have one caller 'serve', got %v", rootNode.Children)
	}
	serveNode := rootNode.Children[0]
	if len(serveNode.Children) != 1 || serveNode.Children[0].Symbol.Name != "main" {
		t.Errorf("serve should have one caller 'main', got %v", serveNode.Children)
	}
}

func TestTrace_CycleDetection(t *testing.T) {
	ping := &parser.Symbol{Name: "ping", Kind: parser.KindFunction, File: "/src/game.go", StartLine: 1, EndLine: 10}
	pong := &parser.Symbol{Name: "pong", Kind: parser.KindFunction, File: "/src/game.go", StartLine: 12, EndLine: 20}

	g := &CallGraph{
		Symbols: []*parser.Symbol{ping, pong},
		Edges: []CallEdge{
			{Caller: ping, Callee: pong, CalleeName: "pong", Line: 5},
			{Caller: pong, Callee: ping, CalleeName: "ping", Line: 15},
		},
	}

	result := Trace(context.Background(), g, "ping", TraceOpts{Direction: "callees", MaxDepth: 10})

	if result.TotalNodes < 2 {
		t.Errorf("expected at least 2 nodes, got %d", result.TotalNodes)
	}
	rootNode := result.Tree[0]
	if len(rootNode.Children) != 1 || rootNode.Children[0].Symbol.Name != "pong" {
		t.Fatalf("ping should call pong, got %v", rootNode.Children)
	}
	pongNode := rootNode.Children[0]
	if len(pongNode.Children) != 1 {
		t.Fatalf("pong should have 1 child (cycle back to ping), got %d", len(pongNode.Children))
	}
	cycleChild := pongNode.Children[0]
	if !cycleChild.Cycle {
		t.Errorf("back-edge to ping should be marked as cycle")
	}
	if cycleChild.Symbol.Name != "ping" {
		t.Errorf("cycle child should be ping, got %s", cycleChild.Symbol.Name)
	}
}

func TestTrace_DepthLimit(t *testing.T) {
	const chainLen = 6
	syms := make([]*parser.Symbol, chainLen)
	for i := range chainLen {
		syms[i] = &parser.Symbol{
			Name:      symName(i),
			Kind:      parser.KindFunction,
			File:      "/src/chain.go",
			StartLine: uint32(i*10 + 1),
			EndLine:   uint32(i*10 + 9),
		}
	}
	edges := make([]CallEdge, chainLen-1)
	for i := range chainLen - 1 {
		edges[i] = CallEdge{
			Caller:     syms[i],
			Callee:     syms[i+1],
			CalleeName: syms[i+1].Name,
			Line:       uint32(i*10 + 5),
		}
	}

	g := &CallGraph{Symbols: syms, Edges: edges}
	result := Trace(context.Background(), g, "f0", TraceOpts{Direction: "callees", MaxDepth: 3})

	if result.TotalNodes != 4 {
		t.Errorf("expected 4 nodes (f0-f3), got %d", result.TotalNodes)
	}
	if result.MaxDepth != 3 {
		t.Errorf("expected max depth 3, got %d", result.MaxDepth)
	}
}

func TestTrace_NotFound(t *testing.T) {
	g := &CallGraph{
		Symbols: []*parser.Symbol{
			{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
		},
	}

	result := Trace(context.Background(), g, "nonexistent", TraceOpts{})

	if result.Root != nil {
		t.Errorf("root should be nil for nonexistent symbol, got %v", result.Root)
	}
	if result.TotalNodes != 0 {
		t.Errorf("total nodes should be 0, got %d", result.TotalNodes)
	}
}

func symName(i int) string {
	return "f" + string(rune('0'+i))
}

// Issue #867: a bare name matching 2+ functions returns NO merged tree —
// the old first-match pick merged the call trees of every `main`/`Close`
// in the repo into an answer that is wrong for all of them.
func TestTrace_AmbiguousRoot_NoTree(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/cmd/api/main.go", StartLine: 1, EndLine: 20},
		{Name: "main", Kind: parser.KindFunction, File: "/src/cmd/worker/main.go", StartLine: 1, EndLine: 20},
		{Name: "helper", Kind: parser.KindFunction, File: "/src/cmd/api/helper.go", StartLine: 1, EndLine: 10},
	}
	g := &CallGraph{Symbols: symbols}

	res := Trace(context.Background(), g, "main", TraceOpts{Direction: "callees"})

	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected 2 ambiguous matches, got %d", len(res.Ambiguous))
	}
	if res.Root != nil || len(res.Tree) != 0 {
		t.Fatal("ambiguous root must not produce a merged tree")
	}
}

// The "Receiver.Name" qualified input resolves to exactly the matching
// method — a qualified query must not degrade to the bare-name merge.
func TestTrace_QualifiedRoot_ResolvesReceiver(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "Serve", Kind: parser.KindMethod, Receiver: "Server", File: "/src/api/server.go", StartLine: 10, EndLine: 30},
		{Name: "Serve", Kind: parser.KindMethod, Receiver: "Mux", File: "/src/mux/mux.go", StartLine: 5, EndLine: 15},
	}
	g := &CallGraph{Symbols: symbols}

	res := Trace(context.Background(), g, "Server.Serve", TraceOpts{Direction: "callees"})

	if len(res.Ambiguous) != 0 {
		t.Fatalf("qualified query must not be ambiguous, got %d matches", len(res.Ambiguous))
	}
	if res.Root == nil || res.Root.File != "/src/api/server.go" {
		t.Fatalf("qualified query must resolve to Server.Serve, got %v", res.Root)
	}
}

// A qualified name whose receiver exists but points at the wrong method
// still resolves exactly — qualified matching is exact, not fuzzy.
func TestFindSymbols_QualifiedExactMatch(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "Close", Kind: parser.KindMethod, Receiver: "DB", File: "/src/db.go", StartLine: 1, EndLine: 5},
		{Name: "Close", Kind: parser.KindMethod, Receiver: "Cache", File: "/src/cache.go", StartLine: 1, EndLine: 5},
		{Name: "Close", Kind: parser.KindFunction, File: "/src/util.go", StartLine: 1, EndLine: 5},
	}
	matches := FindSymbols(symbols, "Cache.Close")
	if len(matches) != 1 || matches[0].File != "/src/cache.go" {
		t.Fatalf("Cache.Close must match exactly the Cache method, got %v", matches)
	}
	// Bare-name lookup still sees all three.
	if got := len(FindSymbols(symbols, "Close")); got != 3 {
		t.Fatalf("bare Close must match 3, got %d", got)
	}
}
