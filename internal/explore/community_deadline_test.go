package explore

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestBuildCommunityOverview_CanceledCtx_Skipped verifies the #534 fix: a
// pre-canceled ctx makes buildCommunityOverview return nil (communities
// skipped) instead of running the full CPU-bound Louvain detection. RED-on-
// revert: remove the ctx.Err() guard / ctx param and Louvain runs unbounded.
func TestBuildCommunityOverview_CanceledCtx_Skipped(t *testing.T) {
	syms := []*parser.Symbol{
		{Name: "a", Kind: parser.KindFunction, File: "/repo/a.go"},
		{Name: "b", Kind: parser.KindFunction, File: "/repo/b.go"},
		{Name: "c", Kind: parser.KindFunction, File: "/repo/c.go"},
		{Name: "d", Kind: parser.KindFunction, File: "/repo/d.go"},
	}
	edges := []callgraph.CallEdge{
		{Caller: syms[0], Callee: syms[1]},
		{Caller: syms[1], Callee: syms[0]},
		{Caller: syms[2], Callee: syms[3]},
		{Caller: syms[3], Callee: syms[2]},
	}
	cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	overview := buildCommunityOverview(ctx, cg, "/repo")
	if overview != nil {
		t.Fatalf("canceled ctx must skip community detection, got %+v", overview)
	}
}
