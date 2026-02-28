package callgraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestBuildCallGraph_SameFile(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
		{Name: "helper", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 12, EndLine: 20},
	}
	calls := []parser.CallSite{
		{Name: "helper", File: "/src/main.go", Line: 5},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	e := g.Edges[0]
	if e.Caller == nil || e.Caller.Name != "main" {
		t.Errorf("caller should be main, got %v", e.Caller)
	}
	if e.Callee == nil || e.Callee.Name != "helper" {
		t.Errorf("callee should be helper, got %v", e.Callee)
	}
	if e.CalleeName != "helper" {
		t.Errorf("calleeName should be helper, got %s", e.CalleeName)
	}
}

func TestBuildCallGraph_CrossPackage(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/project/cmd/main.go", StartLine: 1, EndLine: 10},
		{Name: "Serve", Kind: parser.KindFunction, File: "/project/internal/server/server.go", StartLine: 1, EndLine: 30},
	}
	calls := []parser.CallSite{
		{Name: "Serve", File: "/project/cmd/main.go", Line: 5},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	e := g.Edges[0]
	if e.Caller == nil || e.Caller.Name != "main" {
		t.Errorf("caller should be main, got %v", e.Caller)
	}
	if e.Callee == nil || e.Callee.Name != "Serve" {
		t.Errorf("callee should be Serve, got %v", e.Callee)
	}
}

func TestBuildCallGraph_Unresolved(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
	}
	calls := []parser.CallSite{
		{Name: "Println", Receiver: "fmt", File: "/src/main.go", Line: 5},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	e := g.Edges[0]
	if e.Callee != nil {
		t.Errorf("callee should be nil for unresolved call, got %v", e.Callee)
	}
	if e.CalleeName != "Println" {
		t.Errorf("calleeName should be Println, got %s", e.CalleeName)
	}
	if e.Receiver != "fmt" {
		t.Errorf("receiver should be fmt, got %s", e.Receiver)
	}
}

func TestBuildCallGraph_FindCaller(t *testing.T) {
	// outer spans lines 1-20, inner spans lines 5-8.
	// A call at line 15 is inside outer but outside inner → caller should be outer.
	symbols := []*parser.Symbol{
		{Name: "outer", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 20},
		{Name: "inner", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 5, EndLine: 8},
		{Name: "target", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 22, EndLine: 25},
	}
	calls := []parser.CallSite{
		{Name: "target", File: "/src/main.go", Line: 15},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	e := g.Edges[0]
	if e.Caller == nil || e.Caller.Name != "outer" {
		t.Errorf("caller should be outer, got %v", e.Caller)
	}

	// Also test that a call at line 6 (inside inner) resolves to inner (narrowest).
	calls2 := []parser.CallSite{
		{Name: "target", File: "/src/main.go", Line: 6},
	}
	g2 := BuildCallGraph(symbols, calls2)
	e2 := g2.Edges[0]
	if e2.Caller == nil || e2.Caller.Name != "inner" {
		t.Errorf("caller should be inner (narrowest), got %v", e2.Caller)
	}
}
