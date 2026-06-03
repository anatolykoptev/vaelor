package codegraph

import (
	"sort"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// helper: build a minimal *parser.Symbol.
func sym(name, file string, isPublic bool) *parser.Symbol {
	return &parser.Symbol{
		Name:      name,
		File:      file,
		Kind:      parser.KindFunction,
		IsPublic:  isPublic,
		StartLine: 1,
		EndLine:   10,
	}
}

// helper: build a callgraph edge.
func callEdge(caller, callee *parser.Symbol) callgraph.CallEdge {
	return callgraph.CallEdge{Caller: caller, Callee: callee, CalleeName: callee.Name}
}

// buildTestCG builds a minimal CallGraph from explicit edge pairs.
func buildTestCG(syms []*parser.Symbol, edges []callgraph.CallEdge) *callgraph.CallGraph {
	return &callgraph.CallGraph{Edges: edges, Symbols: syms}
}

// communityMap turns the vertex list into the same map ExtractFlows expects.
func communityMap(vertices []vertexData) map[string]int {
	m := make(map[string]int)
	for _, v := range vertices {
		if v.Label != "Symbol" {
			continue
		}
		key := v.Props["name"] + compositeKeyDelim + v.Props["file"] //nolint:gocritic
		cid := 0
		if v.Props["community"] != "" {
			for _, c := range v.Props["community"] {
				cid = int(c - '0')
				break
			}
		}
		m[key] = cid
	}
	return m
}

// makeVertices builds a minimal vertexData slice from a symbol list, all in community 0.
func makeVertices(syms []*parser.Symbol, root string) []vertexData {
	verts := make([]vertexData, len(syms))
	for i, s := range syms {
		verts[i] = vertexData{
			Label: "Symbol",
			Props: map[string]string{
				"name":      s.Name,
				"file":      relPath(s.File, root),
				"community": "0",
			},
		}
	}
	return verts
}

// makeVerticesMultiCommunity assigns community by index into communities slice.
func makeVerticesMultiCommunity(syms []*parser.Symbol, root string, communities []int) []vertexData {
	verts := make([]vertexData, len(syms))
	for i, s := range syms {
		c := 0
		if i < len(communities) {
			c = communities[i]
		}
		cid := string(rune('0' + c))
		verts[i] = vertexData{
			Label: "Symbol",
			Props: map[string]string{
				"name":      s.Name,
				"file":      relPath(s.File, root),
				"community": cid,
			},
		}
	}
	return verts
}

// TestExtractFlows_EntryPointFromZeroCaller verifies that a zero-in-edge exported
// symbol is detected as an entry-point. This test goes RED when ExtractFlows is
// not implemented.
func TestExtractFlows_EntryPointFromZeroCaller(t *testing.T) {
	root := "/repo"
	entry := sym("PublicAPI", root+"/api.go", true)
	leaf := sym("helper", root+"/api.go", false)

	cg := buildTestCG([]*parser.Symbol{entry, leaf}, []callgraph.CallEdge{
		callEdge(entry, leaf),
	})

	verts := makeVertices([]*parser.Symbol{entry, leaf}, root)
	communities := communityMap(verts)
	pr := map[string]float64{
		entry.Name + compositeKeyDelim + relPath(entry.File, root): 0.5,
		leaf.Name + compositeKeyDelim + relPath(leaf.File, root):   0.1,
	}

	// No HANDLES edges — only zero-caller exported symbol path.
	flows := ExtractFlows(root, cg, communities, pr, nil)

	if len(flows) == 0 {
		t.Fatal("expected at least one flow from zero-caller exported entry-point, got none")
	}
	if flows[0].EntrySym != entry.Name {
		t.Errorf("entry sym = %q, want %q", flows[0].EntrySym, entry.Name)
	}
}

// TestExtractFlows_EntryPointFromHANDLES verifies that a symbol targeted by a
// HANDLES edge is detected as an entry-point even if it has callers.
func TestExtractFlows_EntryPointFromHANDLES(t *testing.T) {
	root := "/repo"
	handler := sym("handleSearch", root+"/server.go", false)
	leaf := sym("doQuery", root+"/db.go", false)
	other := sym("caller", root+"/main.go", false)

	cg := buildTestCG(
		[]*parser.Symbol{handler, leaf, other},
		[]callgraph.CallEdge{
			callEdge(handler, leaf),
			callEdge(other, handler), // handler has an in-edge, so not zero-caller
		},
	)

	verts := makeVertices([]*parser.Symbol{handler, leaf, other}, root)
	communities := communityMap(verts)
	pr := map[string]float64{
		handler.Name + compositeKeyDelim + relPath(handler.File, root): 0.6,
		leaf.Name + compositeKeyDelim + relPath(leaf.File, root):       0.2,
		other.Name + compositeKeyDelim + relPath(other.File, root):     0.1,
	}

	// Provide a HANDLES target for handler.
	handlesTargets := map[string]bool{
		handler.Name + compositeKeyDelim + relPath(handler.File, root): true,
	}

	flows := ExtractFlows(root, cg, communities, pr, handlesTargets)

	// handler must be an entry-point from HANDLES even though it has callers.
	found := false
	for _, f := range flows {
		if f.EntrySym == handler.Name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("handler %q not found as entry-point from HANDLES; flows: %v", handler.Name, flows)
	}
}

// TestExtractFlows_FlowNameUsesPageRank verifies that the flow name is
// "<entry> → <leaf>" using the highest-pagerank leaf.
func TestExtractFlows_FlowNameUsesPageRank(t *testing.T) {
	root := "/repo"
	entry := sym("Entry", root+"/a.go", true)
	highPR := sym("Important", root+"/b.go", false)
	lowPR := sym("Minor", root+"/c.go", false)

	cg := buildTestCG(
		[]*parser.Symbol{entry, highPR, lowPR},
		[]callgraph.CallEdge{
			callEdge(entry, highPR),
			callEdge(entry, lowPR),
		},
	)
	verts := makeVertices([]*parser.Symbol{entry, highPR, lowPR}, root)
	communities := communityMap(verts)
	pr := map[string]float64{
		entry.Name + compositeKeyDelim + relPath(entry.File, root):   0.5,
		highPR.Name + compositeKeyDelim + relPath(highPR.File, root): 0.4,
		lowPR.Name + compositeKeyDelim + relPath(lowPR.File, root):   0.05,
	}

	flows := ExtractFlows(root, cg, communities, pr, nil)
	if len(flows) == 0 {
		t.Fatal("no flows produced")
	}
	// The dominant leaf should be highPR (higher pagerank).
	f := flows[0]
	if f.LeafSym != highPR.Name {
		t.Errorf("leaf sym = %q, want %q (highest PR leaf)", f.LeafSym, highPR.Name)
	}
	if !strings.Contains(f.Name, "→") {
		t.Errorf("flow name %q does not contain '→'", f.Name)
	}
}

// TestExtractFlows_FlowsMaxCap verifies that flows are capped at flowsMax.
func TestExtractFlows_FlowsMaxCap(t *testing.T) {
	root := "/repo"

	// Build flowsMax+5 isolated entry-points, each with one leaf.
	count := flowsMax + 5
	syms := make([]*parser.Symbol, 0, count*2)
	edges := make([]callgraph.CallEdge, 0, count)
	for i := range count {
		entry := sym(exportedName(i), root+"/a.go", true)
		leaf := sym(unexportedName(i), root+"/b.go", false)
		syms = append(syms, entry, leaf)
		edges = append(edges, callEdge(entry, leaf))
	}
	cg := buildTestCG(syms, edges)
	verts := makeVertices(syms, root)
	communities := communityMap(verts)
	pr := make(map[string]float64, len(syms))
	for i, s := range syms {
		pr[s.Name+compositeKeyDelim+relPath(s.File, root)] = float64(len(syms)-i) * 0.01
	}

	flows := ExtractFlows(root, cg, communities, pr, nil)
	if len(flows) > flowsMax {
		t.Errorf("flows count %d exceeds flowsMax %d", len(flows), flowsMax)
	}
}

// TestExtractFlows_StaysInCommunity verifies that chains stay mostly within
// one Louvain community (inter-community traversal should not happen).
func TestExtractFlows_StaysInCommunity(t *testing.T) {
	root := "/repo"
	// Community 0: entry + leaf0
	entry := sym("EntryC0", root+"/a.go", true)
	leaf0 := sym("LeafC0", root+"/a.go", false)
	// Community 1: leaf1 (different community)
	leaf1 := sym("LeafC1", root+"/b.go", false)

	cg := buildTestCG(
		[]*parser.Symbol{entry, leaf0, leaf1},
		[]callgraph.CallEdge{
			callEdge(entry, leaf0),
			callEdge(entry, leaf1),
		},
	)
	// entry and leaf0 in community 0, leaf1 in community 1.
	verts := makeVerticesMultiCommunity(
		[]*parser.Symbol{entry, leaf0, leaf1},
		root,
		[]int{0, 0, 1},
	)
	communities := communityMap(verts)
	pr := map[string]float64{
		entry.Name + compositeKeyDelim + relPath(entry.File, root): 0.5,
		leaf0.Name + compositeKeyDelim + relPath(leaf0.File, root): 0.3,
		leaf1.Name + compositeKeyDelim + relPath(leaf1.File, root): 0.8, // higher PR but diff community
	}

	flows := ExtractFlows(root, cg, communities, pr, nil)
	for _, f := range flows {
		if f.EntrySym != entry.Name {
			continue
		}
		// The flow should prefer leaf0 (same community) as the dominant leaf.
		if f.LeafSym == leaf1.Name {
			t.Errorf("flow crossed community boundary to leaf1; entry=%q, leaf=%q", f.EntrySym, f.LeafSym)
		}
	}
}

// TestExtractFlows_DepthBound verifies that DFS is bounded by flowsDFSDepth.
func TestExtractFlows_DepthBound(t *testing.T) {
	root := "/repo"
	// Chain: entry → n1 → n2 → ... → nN where N > flowsDFSDepth.
	depth := flowsDFSDepth + 3
	syms := make([]*parser.Symbol, depth+1)
	syms[0] = sym("ChainEntry", root+"/a.go", true)
	for i := range depth {
		syms[i+1] = sym(exportedName(i), root+"/chain.go", true)
	}
	edges := make([]callgraph.CallEdge, depth)
	for i := range depth {
		edges[i] = callEdge(syms[i], syms[i+1])
	}
	cg := buildTestCG(syms, edges)
	verts := makeVertices(syms, root)
	communities := communityMap(verts)
	pr := make(map[string]float64)
	for i, s := range syms {
		pr[s.Name+compositeKeyDelim+relPath(s.File, root)] = float64(len(syms)-i) * 0.1
	}

	flows := ExtractFlows(root, cg, communities, pr, nil)
	for _, f := range flows {
		if len(f.MemberSyms) > flowsDFSDepth+2 {
			t.Errorf("member_syms length %d exceeds depth bound %d+2", len(f.MemberSyms), flowsDFSDepth)
		}
	}
}

// TestExtractFlows_DeterministicOrdering verifies that two calls with identical
// input produce the same flow ordering.
func TestExtractFlows_DeterministicOrdering(t *testing.T) {
	root := "/repo"
	syms := []*parser.Symbol{
		sym("Alpha", root+"/a.go", true),
		sym("Beta", root+"/b.go", true),
		sym("leafA", root+"/a.go", false),
		sym("leafB", root+"/b.go", false),
	}
	edges := []callgraph.CallEdge{
		callEdge(syms[0], syms[2]),
		callEdge(syms[1], syms[3]),
	}
	cg := buildTestCG(syms, edges)
	verts := makeVertices(syms, root)
	communities := communityMap(verts)
	pr := map[string]float64{
		syms[0].Name + compositeKeyDelim + relPath(syms[0].File, root): 0.4,
		syms[1].Name + compositeKeyDelim + relPath(syms[1].File, root): 0.6,
		syms[2].Name + compositeKeyDelim + relPath(syms[2].File, root): 0.1,
		syms[3].Name + compositeKeyDelim + relPath(syms[3].File, root): 0.2,
	}

	flows1 := ExtractFlows(root, cg, communities, pr, nil)
	flows2 := ExtractFlows(root, cg, communities, pr, nil)

	if len(flows1) != len(flows2) {
		t.Fatalf("non-deterministic: len %d vs %d", len(flows1), len(flows2))
	}
	for i := range flows1 {
		if flows1[i].Name != flows2[i].Name {
			t.Errorf("position %d: %q vs %q", i, flows1[i].Name, flows2[i].Name)
		}
	}
}

// TestExtractFlows_PrioritySorting verifies flows are sorted by priority descending.
func TestExtractFlows_PrioritySorting(t *testing.T) {
	root := "/repo"
	// Two independent entry-points with different PR.
	lowEntry := sym("LowPrio", root+"/a.go", true)
	highEntry := sym("HighPrio", root+"/b.go", true)
	leafL := sym("leafLow", root+"/a.go", false)
	leafH := sym("leafHigh", root+"/b.go", false)

	cg := buildTestCG(
		[]*parser.Symbol{lowEntry, highEntry, leafL, leafH},
		[]callgraph.CallEdge{
			callEdge(lowEntry, leafL),
			callEdge(highEntry, leafH),
		},
	)
	verts := makeVertices([]*parser.Symbol{lowEntry, highEntry, leafL, leafH}, root)
	communities := communityMap(verts)
	pr := map[string]float64{
		lowEntry.Name + compositeKeyDelim + relPath(lowEntry.File, root):   0.1,
		highEntry.Name + compositeKeyDelim + relPath(highEntry.File, root): 0.9,
		leafL.Name + compositeKeyDelim + relPath(leafL.File, root):         0.05,
		leafH.Name + compositeKeyDelim + relPath(leafH.File, root):         0.05,
	}

	flows := ExtractFlows(root, cg, communities, pr, nil)
	if len(flows) < 2 {
		t.Fatalf("need at least 2 flows, got %d", len(flows))
	}
	if !sort.SliceIsSorted(flows, func(i, j int) bool {
		return flows[i].Priority > flows[j].Priority
	}) {
		t.Errorf("flows not sorted by priority desc: %v", flows)
	}
}

// TestExtractFlows_EmptyGraph verifies graceful handling of an empty call graph.
func TestExtractFlows_EmptyGraph(t *testing.T) {
	root := "/repo"
	cg := buildTestCG(nil, nil)
	flows := ExtractFlows(root, cg, nil, nil, nil)
	if flows == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

// TestFlow_StructFields verifies that the Flow struct has the fields required
// by ADR-001 and the plan.
func TestFlow_StructFields(t *testing.T) {
	f := Flow{
		FlowID:     "id",
		Name:       "A → B",
		EntrySym:   "A",
		EntryFile:  "a.go",
		LeafSym:    "B",
		MemberSyms: []string{"A", "B"},
		Priority:   0.5,
		Community:  "0",
	}
	if f.Name == "" || f.EntrySym == "" || f.LeafSym == "" {
		t.Error("required Flow fields are empty")
	}
}

// --- helpers for generating unique names in table tests ---

func exportedName(i int) string {
	// Uppercase first char so it counts as exported.
	name := []rune("Entry")
	return string(append(name, []rune(intToAlpha(i))...))
}

func unexportedName(i int) string {
	name := []rune("leaf")
	return string(append(name, []rune(intToAlpha(i))...))
}

func intToAlpha(n int) string {
	if n == 0 {
		return "A"
	}
	var out []rune
	for n > 0 {
		out = append([]rune{rune('A' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}
