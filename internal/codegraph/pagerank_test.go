package codegraph

import (
	"math"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/ranking"
)

func TestComputeSymbolPageRank(t *testing.T) {
	t.Parallel()

	symA := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/repo/cmd/main.go"}
	symB := &parser.Symbol{Name: "handler", Kind: parser.KindFunction, File: "/repo/internal/handler.go"}
	symC := &parser.Symbol{Name: "helper", Kind: parser.KindFunction, File: "/repo/internal/helper.go"}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{symA, symB, symC},
		Edges: []callgraph.CallEdge{
			{Caller: symA, Callee: symB},
			{Caller: symA, Callee: symC},
			{Caller: symB, Callee: symC},
		},
	}

	scores := computeSymbolPageRank("/repo", cg.Symbols, cg, nil)
	if scores == nil {
		t.Fatal("expected non-nil scores")
	}

	// helper has most incoming edges (called by main and handler).
	helperKey := "helper" + compositeKeyDelim + "internal/helper.go"
	handlerKey := "handler" + compositeKeyDelim + "internal/handler.go"

	if scores[helperKey] <= scores[handlerKey] {
		t.Errorf("helper (%.4f) should rank higher than handler (%.4f)",
			scores[helperKey], scores[handlerKey])
	}
}

func TestComputeSymbolPageRankEmpty(t *testing.T) {
	t.Parallel()
	cg := &callgraph.CallGraph{}
	scores := computeSymbolPageRank("/repo", nil, cg, nil)
	if scores != nil {
		t.Errorf("expected nil for empty graph, got %v", scores)
	}
}

// TestComputeSymbolPageRank_PersonalizationBoostsSeed verifies the core change:
// a query-seeded symbol and its direct neighbor rank strictly higher under
// WeightedPersonalizedPageRank(seeds) than under the unpersonalized fallback.
//
// Graph: a hub "core" with high in-degree (4 callers) dominates unpersonalized
// PageRank, leaving "handler" modest. Seeding "handler" concentrates teleport
// mass on it and its callee "helper", boosting both above their unpersonalized
// scores.
func TestComputeSymbolPageRank_PersonalizationBoostsSeed(t *testing.T) {
	t.Parallel()

	mkSym := func(name, file string) *parser.Symbol {
		return &parser.Symbol{Name: name, Kind: parser.KindFunction, File: file}
	}
	symMain := mkSym("main", "/repo/cmd/main.go")
	symHandler := mkSym("handler", "/repo/internal/handler.go")
	symHelper := mkSym("helper", "/repo/internal/helper.go")
	symCore := mkSym("core", "/repo/internal/core.go")
	symA := mkSym("a", "/repo/internal/a.go")
	symB := mkSym("b", "/repo/internal/b.go")
	symC := mkSym("c", "/repo/internal/c.go")
	symD := mkSym("d", "/repo/internal/d.go")

	syms := []*parser.Symbol{symMain, symHandler, symHelper, symCore, symA, symB, symC, symD}
	cg := &callgraph.CallGraph{
		Symbols: syms,
		Edges: []callgraph.CallEdge{
			{Caller: symMain, Callee: symHandler},
			{Caller: symHandler, Callee: symHelper},
			{Caller: symHandler, Callee: symCore},
			{Caller: symA, Callee: symCore},
			{Caller: symB, Callee: symCore},
			{Caller: symC, Callee: symCore},
			{Caller: symD, Callee: symCore},
		},
	}

	handlerKey := "handler" + compositeKeyDelim + "internal/handler.go"
	helperKey := "helper" + compositeKeyDelim + "internal/helper.go"
	seeds := map[string]float64{handlerKey: seedPersonalizationWeight}

	unpersonalized := computeSymbolPageRank("/repo", syms, cg, nil)
	personalized := computeSymbolPageRank("/repo", syms, cg, seeds)

	if personalized[handlerKey] <= unpersonalized[handlerKey] {
		t.Errorf("seeded handler (%.6f) should outrank unpersonalized handler (%.6f)",
			personalized[handlerKey], unpersonalized[handlerKey])
	}
	if personalized[helperKey] <= unpersonalized[helperKey] {
		t.Errorf("seeded neighbor helper (%.6f) should outrank unpersonalized helper (%.6f)",
			personalized[helperKey], unpersonalized[helperKey])
	}
}

// TestComputeSymbolPageRank_EmptySeedFallbackIdentical verifies the no-regression
// contract: with no seeds the computation is byte-identical to the pre-PPR
// unpersonalized unweighted PageRank (20 iterations, 0.85 damping). Guards
// against a silent drift to weighted edges or 40 iterations on the fallback path.
//
// Uses a 25-node chain (n0→n1→…→n24) which converges slowly — rank propagates
// one hop per iteration, so 20 vs 40 iterations produce measurably different
// tail-node scores. This makes the iteration-count guard effective (a fast-
// converging small graph could not distinguish 20 from 40).
func TestComputeSymbolPageRank_EmptySeedFallbackIdentical(t *testing.T) {
	t.Parallel()

	const chainLen = 25
	syms := make([]*parser.Symbol, chainLen)
	for i := range chainLen {
		syms[i] = &parser.Symbol{
			Name: "n" + itoa(i),
			Kind: parser.KindFunction,
			File: "/repo/chain.go",
		}
	}
	var edges []callgraph.CallEdge
	for i := 0; i < chainLen-1; i++ {
		edges = append(edges, callgraph.CallEdge{Caller: syms[i], Callee: syms[i+1]})
	}
	cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

	got := computeSymbolPageRank("/repo", syms, cg, nil)
	want := ranking.PageRank(buildUnweightedCallGraph("/repo", syms, cg), pagerankIterations, pagerankDamping)

	if len(got) != len(want) {
		t.Fatalf("key count mismatch: got=%d want=%d", len(got), len(want))
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("missing key %q in fallback result", k)
		}
		if math.Abs(gv-wv) > 1e-9 {
			t.Errorf("key %q: fallback=%.9f want=%.9f (must be byte-identical)", k, gv, wv)
		}
	}
}

// TestBuildWeightedCallGraph_Heuristics verifies the Aider repomap.py edge-weight
// heuristics: sqrt(num_refs) damping, private (_prefix) discount, common-name
// (>5 definitions) discount, and seed-match boost.
func TestBuildWeightedCallGraph_Heuristics(t *testing.T) {
	t.Parallel()

	t.Run("sqrt_num_refs_damping", func(t *testing.T) {
		t.Parallel()
		// big has 100 inbound refs; small has 4. Edge weight ratio must be
		// sqrt(100)/sqrt(4) = 5:1, NOT the raw 100/4 = 25:1.
		var syms []*parser.Symbol
		var edges []callgraph.CallEdge
		for i := 0; i < 100; i++ {
			c := &parser.Symbol{Name: "bigCaller" + itoa(i), Kind: parser.KindFunction, File: "/repo/big.go"}
			syms = append(syms, c)
			edges = append(edges, callgraph.CallEdge{Caller: c, Callee: &parser.Symbol{Name: "big", Kind: parser.KindFunction, File: "/repo/big.go"}})
		}
		for i := 0; i < 4; i++ {
			c := &parser.Symbol{Name: "smallCaller" + itoa(i), Kind: parser.KindFunction, File: "/repo/small.go"}
			syms = append(syms, c)
			edges = append(edges, callgraph.CallEdge{Caller: c, Callee: &parser.Symbol{Name: "small", Kind: parser.KindFunction, File: "/repo/small.go"}})
		}
		syms = append(syms, &parser.Symbol{Name: "big", Kind: parser.KindFunction, File: "/repo/big.go"})
		syms = append(syms, &parser.Symbol{Name: "small", Kind: parser.KindFunction, File: "/repo/small.go"})
		cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

		w := buildWeightedCallGraph("/repo", syms, cg, nil)
		bigCaller0Key := "bigCaller0" + compositeKeyDelim + "big.go"
		bigKey := "big" + compositeKeyDelim + "big.go"
		smallCaller0Key := "smallCaller0" + compositeKeyDelim + "small.go"
		smallKey := "small" + compositeKeyDelim + "small.go"

		bigW := w[bigCaller0Key][bigKey]
		smallW := w[smallCaller0Key][smallKey]
		ratio := bigW / smallW
		if math.Abs(ratio-5.0) > 0.01 {
			t.Errorf("sqrt damping: big/small ratio=%.4f, want 5.0 (sqrt(100)/sqrt(4)), not 25.0", ratio)
		}
	})

	t.Run("private_discount", func(t *testing.T) {
		t.Parallel()
		// _priv and pub both have in-degree 1, unique names, no seeds.
		// _priv edge weight = sqrt(1)*0.1 = 0.1; pub edge weight = sqrt(1)*1 = 1.0.
		callerPub := &parser.Symbol{Name: "callerPub", Kind: parser.KindFunction, File: "/repo/x.go"}
		callerPriv := &parser.Symbol{Name: "callerPriv", Kind: parser.KindFunction, File: "/repo/y.go"}
		symPriv := &parser.Symbol{Name: "_priv", Kind: parser.KindFunction, File: "/repo/p.go"}
		symPub := &parser.Symbol{Name: "pub", Kind: parser.KindFunction, File: "/repo/q.go"}
		syms := []*parser.Symbol{callerPub, callerPriv, symPriv, symPub}
		cg := &callgraph.CallGraph{
			Symbols: syms,
			Edges: []callgraph.CallEdge{
				{Caller: callerPriv, Callee: symPriv},
				{Caller: callerPub, Callee: symPub},
			},
		}
		w := buildWeightedCallGraph("/repo", syms, cg, nil)
		privW := w["callerPriv"+compositeKeyDelim+"y.go"]["_priv"+compositeKeyDelim+"p.go"]
		pubW := w["callerPub"+compositeKeyDelim+"x.go"]["pub"+compositeKeyDelim+"q.go"]
		if math.Abs(privW-0.1*pubW) > 1e-9 {
			t.Errorf("private discount: _priv=%.6f pub=%.6f, want _priv = 0.1*pub", privW, pubW)
		}
	})

	t.Run("common_name_discount", func(t *testing.T) {
		t.Parallel()
		// "init" is defined in 6 files (>5) → common-name ×0.1 discount.
		// "unique" defined once → no discount. Both in-degree 1, no seeds.
		var syms []*parser.Symbol
		var edges []callgraph.CallEdge
		callerInit := &parser.Symbol{Name: "callerInit", Kind: parser.KindFunction, File: "/repo/c.go"}
		callerUnique := &parser.Symbol{Name: "callerUnique", Kind: parser.KindFunction, File: "/repo/c2.go"}
		syms = append(syms, callerInit, callerUnique)
		// 6 distinct "init" symbols in different files.
		for i := 0; i < 6; i++ {
			s := &parser.Symbol{Name: "init", Kind: parser.KindFunction, File: "/repo/f" + itoa(i) + ".go"}
			syms = append(syms, s)
			if i == 0 {
				edges = append(edges, callgraph.CallEdge{Caller: callerInit, Callee: s})
			}
		}
		symUnique := &parser.Symbol{Name: "unique", Kind: parser.KindFunction, File: "/repo/u.go"}
		syms = append(syms, symUnique)
		edges = append(edges, callgraph.CallEdge{Caller: callerUnique, Callee: symUnique})
		cg := &callgraph.CallGraph{Symbols: syms, Edges: edges}

		w := buildWeightedCallGraph("/repo", syms, cg, nil)
		commonW := w["callerInit"+compositeKeyDelim+"c.go"]["init"+compositeKeyDelim+"f0.go"]
		uniqueW := w["callerUnique"+compositeKeyDelim+"c2.go"]["unique"+compositeKeyDelim+"u.go"]
		if math.Abs(commonW-0.1*uniqueW) > 1e-9 {
			t.Errorf("common-name discount: init=%.6f unique=%.6f, want init = 0.1*unique", commonW, uniqueW)
		}
	})

	t.Run("seed_match_boost", func(t *testing.T) {
		t.Parallel()
		// "target" has in-degree 1, unique name. Seeding it → edge weight ×10.
		caller := &parser.Symbol{Name: "caller", Kind: parser.KindFunction, File: "/repo/c.go"}
		target := &parser.Symbol{Name: "target", Kind: parser.KindFunction, File: "/repo/t.go"}
		other := &parser.Symbol{Name: "other", Kind: parser.KindFunction, File: "/repo/o.go"}
		caller2 := &parser.Symbol{Name: "caller2", Kind: parser.KindFunction, File: "/repo/c2.go"}
		syms := []*parser.Symbol{caller, target, other, caller2}
		cg := &callgraph.CallGraph{
			Symbols: syms,
			Edges: []callgraph.CallEdge{
				{Caller: caller, Callee: target},
				{Caller: caller2, Callee: other},
			},
		}
		targetKey := "target" + compositeKeyDelim + "t.go"
		seeds := map[string]float64{targetKey: seedPersonalizationWeight}
		w := buildWeightedCallGraph("/repo", syms, cg, seeds)
		targetW := w["caller"+compositeKeyDelim+"c.go"][targetKey]
		otherW := w["caller2"+compositeKeyDelim+"c2.go"]["other"+compositeKeyDelim+"o.go"]
		if math.Abs(targetW-10.0*otherW) > 1e-9 {
			t.Errorf("seed-match boost: target=%.6f other=%.6f, want target = 10*other", targetW, otherW)
		}
	})
}

// itoa is a tiny strconv-free int→string for test symbol naming.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
