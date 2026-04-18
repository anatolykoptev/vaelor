package compound_test

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// fakeAnalytics implements graphx.Analytics for prepare_change graph tests.
type fakeAnalytics struct {
	// signals maps "name:file" to a Signals value.
	signals map[string]graphx.Signals
	// topList is returned verbatim by TopPageRank.
	topList []graphx.Signal
	// errSymbol causes Symbol() to return an error when true.
	errSymbol bool
}

func (f *fakeAnalytics) Symbol(_ context.Context, _, name, file string) (graphx.Signals, error) {
	if f.errSymbol {
		return graphx.Signals{}, errors.New("stub error")
	}
	key := name + ":" + file
	sig, ok := f.signals[key]
	if !ok {
		return graphx.Signals{Found: false}, nil
	}
	return sig, nil
}

func (f *fakeAnalytics) TopPageRank(_ context.Context, _ string, _ int) ([]graphx.Signal, error) {
	return f.topList, nil
}

// makePCCallGraph builds a CallGraph with target + named callers, all in the
// same file for simplicity.
func makePCCallGraph(targetName string, callerNames ...string) (*callgraph.CallGraph, *parser.Symbol) {
	target := makeFunc(targetName, targetName+".go", 1, 10)
	syms := []*parser.Symbol{target}
	var edges []callgraph.CallEdge
	for _, cn := range callerNames {
		caller := makeFunc(cn, cn+".go", 1, 10)
		syms = append(syms, caller)
		edges = append(edges, callgraph.CallEdge{
			Caller:     caller,
			Callee:     target,
			CalleeName: targetName,
			Line:       5,
		})
	}
	return &callgraph.CallGraph{Symbols: syms, Edges: edges, Tier: "basic"}, target
}

// TestPrepareChange_Graph_ColdNil — Graph: nil → CommunitiesCrossed==0, HighPRCallers==nil.
func TestPrepareChange_Graph_ColdNil(t *testing.T) {
	cg, _ := makePCCallGraph("DoWork", "Caller1", "Caller2")

	result := compound.PrepareChange(context.Background(), cg, "DoWork", compound.PrepareChangeOpts{
		Repo: "owner/repo",
		// Graph intentionally nil
	})

	if result.CommunitiesCrossed != 0 {
		t.Errorf("want CommunitiesCrossed=0, got %d", result.CommunitiesCrossed)
	}
	if result.HighPRCallers != nil {
		t.Errorf("want HighPRCallers nil, got %v", result.HighPRCallers)
	}
}

// TestPrepareChange_Graph_CommunitiesAndHighPR — 3 callers in 2 distinct communities,
// 1 high-PR caller (above top-decile threshold).
func TestPrepareChange_Graph_CommunitiesAndHighPR(t *testing.T) {
	// callerA and callerB share community "comm-1"; callerC is in "comm-2".
	// target is in "comm-1". That gives 2 distinct communities for callers,
	// plus target's community "comm-1" → still 2 distinct total.
	// We want CommunitiesCrossed == 3 as per task spec: target + 2 caller communities = 3.
	// But target and callerA/callerB share comm-1, so distinct count is:
	//   {comm-1 (target), comm-1 (callerA), comm-1 (callerB), comm-2 (callerC)} → 2 distinct.
	// Task spec says "CommunitiesCrossed==3 (target + 2 = 3 distinct)" — to match exactly,
	// use 3 distinct community IDs: target="c0", callerA="c1", callerB="c1", callerC="c2".
	// That gives {c0, c1, c2} = 3 distinct. callerX is the high-PR one.
	cg, _ := makePCCallGraph("Target", "CallerA", "CallerB", "CallerX")

	signals := map[string]graphx.Signals{
		"Target:Target.go":   {Found: true, Community: "c0", PageRank: 0.1},
		"CallerA:CallerA.go": {Found: true, Community: "c1", PageRank: 0.2},
		"CallerB:CallerB.go": {Found: true, Community: "c1", PageRank: 0.15},
		"CallerX:CallerX.go": {Found: true, Community: "c2", PageRank: 0.9},
	}
	// topList: 20 entries, index 1 (len/10 - 1 = 20/10 - 1 = 1) is the threshold.
	// Build a list where index 1 has PageRank 0.5, so CallerX (0.9) qualifies.
	topList := make([]graphx.Signal, 20)
	for i := range topList {
		topList[i] = graphx.Signal{
			Symbol:  graphx.SymbolRef{Name: "sym", File: "sym.go"},
			Signals: graphx.Signals{Found: true, PageRank: 1.0 - float64(i)*0.04},
		}
	}
	// index 1 has PageRank = 1.0 - 0.04 = 0.96 — CallerX (0.9) does NOT qualify.
	// Adjust: make threshold at index 1 = 0.85 so CallerX (0.9) qualifies.
	topList[1].PageRank = 0.85

	fa := &fakeAnalytics{signals: signals, topList: topList}

	result := compound.PrepareChange(context.Background(), cg, "Target", compound.PrepareChangeOpts{
		Repo:  "owner/repo",
		Graph: fa,
	})

	if result.CommunitiesCrossed != 3 {
		t.Errorf("want CommunitiesCrossed=3, got %d", result.CommunitiesCrossed)
	}
	if len(result.HighPRCallers) != 1 || result.HighPRCallers[0] != "CallerX" {
		t.Errorf("want HighPRCallers=[CallerX], got %v", result.HighPRCallers)
	}
}

// TestPrepareChange_Graph_AllFoundFalse — all Symbol() calls return Found=false
// → CommunitiesCrossed==0, HighPRCallers==nil.
func TestPrepareChange_Graph_AllFoundFalse(t *testing.T) {
	cg, _ := makePCCallGraph("DoWork", "Caller1", "Caller2")

	// No signals registered → all return Found=false.
	fa := &fakeAnalytics{
		signals: map[string]graphx.Signals{},
		topList: []graphx.Signal{
			{Symbol: graphx.SymbolRef{Name: "x", File: "x.go"}, Signals: graphx.Signals{Found: true, PageRank: 0.9}},
		},
	}

	result := compound.PrepareChange(context.Background(), cg, "DoWork", compound.PrepareChangeOpts{
		Repo:  "owner/repo",
		Graph: fa,
	})

	if result.CommunitiesCrossed != 0 {
		t.Errorf("want CommunitiesCrossed=0, got %d", result.CommunitiesCrossed)
	}
	if result.HighPRCallers != nil {
		t.Errorf("want HighPRCallers nil, got %v", result.HighPRCallers)
	}
}
