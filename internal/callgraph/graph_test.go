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

func TestInjectHookEdges(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/plugin.php", StartLine: 1, EndLine: 20},
		{Name: "my_callback", Kind: parser.KindFunction, File: "/src/plugin.php", StartLine: 22, EndLine: 30},
		{Name: "another_cb", Kind: parser.KindFunction, File: "/src/helpers.php", StartLine: 1, EndLine: 10},
	}
	cg := &CallGraph{Symbols: symbols}

	hookRoutes := []HookRoute{
		{Method: "ACTION", Path: "init", Handler: "my_callback", Side: "server"},
		{Method: "ACTION", Path: "init", Handler: "another_cb", Side: "server"},
		{Method: "ACTION", Path: "init", Side: "client", Line: 5},
	}

	InjectHookEdges(cg, hookRoutes)

	// Should create 2 edges: init -> my_callback, init -> another_cb.
	if len(cg.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(cg.Edges))
	}

	names := map[string]bool{}
	for _, e := range cg.Edges {
		if e.Callee == nil {
			t.Error("expected resolved callee, got nil")
			continue
		}
		names[e.Callee.Name] = true
	}
	if !names["my_callback"] {
		t.Error("missing edge to my_callback")
	}
	if !names["another_cb"] {
		t.Error("missing edge to another_cb")
	}
}

func TestInjectHookEdges_Unresolved(t *testing.T) {
	cg := &CallGraph{Symbols: []*parser.Symbol{}}

	hookRoutes := []HookRoute{
		{Method: "ACTION", Path: "init", Handler: "missing_func", Side: "server"},
		{Method: "ACTION", Path: "init", Side: "client", Line: 10},
	}

	InjectHookEdges(cg, hookRoutes)

	// Should still create an edge with nil Callee.
	if len(cg.Edges) != 1 {
		t.Fatalf("expected 1 unresolved edge, got %d", len(cg.Edges))
	}
	if cg.Edges[0].Callee != nil {
		t.Error("expected nil callee for unresolved hook callback")
	}
	if cg.Edges[0].CalleeName != "missing_func" {
		t.Errorf("CalleeName = %q, want missing_func", cg.Edges[0].CalleeName)
	}
}

func TestInjectHookEdges_NoClients(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "my_cb", Kind: parser.KindFunction, File: "/src/p.php", StartLine: 1, EndLine: 5},
	}
	cg := &CallGraph{Symbols: symbols}

	// Only server-side registrations, no client-side invocations.
	// This simulates WP core hooks (admin_notices, init, etc.) where
	// do_action() lives in WordPress core, not in the plugin code.
	hookRoutes := []HookRoute{
		{Method: "ACTION", Path: "init", Handler: "my_cb", Side: "server"},
	}

	InjectHookEdges(cg, hookRoutes)

	// Server-only hooks still get edges — add_action registration proves
	// the callback is alive (WordPress core will invoke it).
	if len(cg.Edges) != 1 {
		t.Fatalf("expected 1 edge (server-only hook), got %d", len(cg.Edges))
	}
	if cg.Edges[0].Callee == nil || cg.Edges[0].Callee.Name != "my_cb" {
		t.Errorf("expected edge to my_cb, got %v", cg.Edges[0].Callee)
	}
}

// TestBuildCallGraph_DropUnresolvedArgRef covers the noise filter:
// IsArgRef CallSites that don't resolve to a function symbol must be
// dropped (vars, member access). Resolved argrefs (real function refs
// passed by name) must be kept.
func TestBuildCallGraph_DropUnresolvedArgRef(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
		{Name: "renderHeading", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 12, EndLine: 14},
	}
	calls := []parser.CallSite{
		// Primary call — always kept.
		{Name: "Register", File: "/src/main.go", Line: 5},
		// Argref that resolves to a real symbol — kept.
		{Name: "renderHeading", File: "/src/main.go", Line: 5, IsArgRef: true},
		// Argref that does NOT resolve — dropped (this is `ctx`, `opts.Slug` etc.).
		{Name: "ctx", File: "/src/main.go", Line: 5, IsArgRef: true},
		{Name: "Slug", Receiver: "opts", File: "/src/main.go", Line: 5, IsArgRef: true},
	}

	g := BuildCallGraph(symbols, calls)

	if len(g.Edges) != 2 {
		t.Fatalf("expected 2 edges (Register + renderHeading), got %d: %+v", len(g.Edges), g.Edges)
	}
	names := map[string]bool{}
	for _, e := range g.Edges {
		names[e.CalleeName] = true
	}
	if !names["Register"] {
		t.Error("primary call Register dropped")
	}
	if !names["renderHeading"] {
		t.Error("resolved argref renderHeading dropped")
	}
	if names["ctx"] || names["Slug"] {
		t.Errorf("unresolved argref leaked: %+v", g.Edges)
	}
}

// TestBuildCallGraph_KeepUnresolvedArgRef_LegacyOptIn covers the
// IncludeFieldAccess opt-in: with field_access=true, unresolved argrefs
// are kept (legacy permissive behaviour).
func TestBuildCallGraph_KeepUnresolvedArgRef_LegacyOptIn(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
	}
	calls := []parser.CallSite{
		{Name: "Register", File: "/src/main.go", Line: 5},
		{Name: "ctx", File: "/src/main.go", Line: 5, IsArgRef: true},
	}

	g := BuildCallGraphWithOpts(symbols, calls, BuildOpts{IncludeFieldAccess: true})

	if len(g.Edges) != 2 {
		t.Fatalf("expected 2 edges with IncludeFieldAccess=true, got %d", len(g.Edges))
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
