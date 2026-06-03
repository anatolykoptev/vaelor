package embeddings

import (
	"math"
	"testing"

	"github.com/anatolykoptev/go-kit/rerank"
)

func TestRRFMerge(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "foo.go", SymbolName: "Foo", Distance: 0.1},
		{FilePath: "bar.go", SymbolName: "Bar", Distance: 0.2},
		{FilePath: "baz.go", SymbolName: "Baz", Distance: 0.3},
	}
	keyword := []KeywordHit{
		{FilePath: "bar.go", SymbolName: "Bar", Line: 10},
		{FilePath: "qux.go", SymbolName: "Qux", Line: 20},
	}

	results := MergeRRF(semantic, keyword, nil, nil, 10, DefaultRRFWeights())

	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	if results[0].SymbolName != "Bar" {
		t.Errorf("expected Bar first (hybrid boost), got %s", results[0].SymbolName)
	}
	if results[0].Source != "hybrid" {
		t.Errorf("expected source=hybrid for Bar, got %s", results[0].Source)
	}

	// Verify remaining sources.
	sourceMap := make(map[string]string)
	for _, r := range results {
		sourceMap[r.SymbolName] = r.Source
	}
	if sourceMap["Foo"] != "semantic" {
		t.Errorf("Foo: expected source=semantic, got %s", sourceMap["Foo"])
	}
	if sourceMap["Qux"] != "keyword" {
		t.Errorf("Qux: expected source=keyword, got %s", sourceMap["Qux"])
	}
}

func TestRRFMergeEmptySemantic(t *testing.T) {
	keyword := []KeywordHit{
		{FilePath: "a.go", SymbolName: "Alpha", Line: 1},
		{FilePath: "b.go", SymbolName: "Beta", Line: 5},
	}

	results := MergeRRF(nil, keyword, nil, nil, 10, DefaultRRFWeights())

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Source != "keyword" {
			t.Errorf("%s: expected source=keyword, got %s", r.SymbolName, r.Source)
		}
	}
}

func TestRRFMergeEmptyKeyword(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "x.go", SymbolName: "X", Distance: 0.1},
		{FilePath: "y.go", SymbolName: "Y", Distance: 0.2},
	}

	results := MergeRRF(semantic, nil, nil, nil, 10, DefaultRRFWeights())

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Source != "semantic" {
			t.Errorf("%s: expected source=semantic, got %s", r.SymbolName, r.Source)
		}
	}
}

func TestRRFMergeTopK(t *testing.T) {
	semantic := make([]SearchResult, 20)
	for i := range 20 {
		semantic[i] = SearchResult{
			FilePath:   "file.go",
			SymbolName: "Sym",
			Distance:   float32(i) * 0.01,
		}
		// Make keys unique.
		semantic[i].FilePath = "file.go"
		semantic[i].StartLine = i
		// FilePath+SymbolName must be unique for dedup — vary the name.
		semantic[i].SymbolName = "Sym" + string(rune('A'+i))
	}

	results := MergeRRF(semantic, nil, nil, nil, 5, DefaultRRFWeights())

	if len(results) != 5 {
		t.Errorf("expected exactly 5 results with topK=5, got %d", len(results))
	}
}

func TestRRFMergeBothEmpty(t *testing.T) {
	results := MergeRRF(nil, nil, nil, nil, 10, DefaultRRFWeights())
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

// TestMergeRRFWeighted_AllOnesEqualsRRF asserts the documented invariant from
// go-kit/rerank/weighted_rrf.go:28 — weights == (1.0, 1.0) is mathematically
// identical to plain rerank.RRF. Locks in the rollback story: env flip to
// 1.0/1.0 reverts to byte-identical pre-Stream-1 behavior.
func TestMergeRRFWeighted_AllOnesEqualsRRF(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "Alpha", Distance: 0.1},
		{FilePath: "b.go", SymbolName: "Beta", Distance: 0.2},
		{FilePath: "c.go", SymbolName: "Gamma", Distance: 0.3},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "Beta", Line: 5},
		{FilePath: "d.go", SymbolName: "Delta", Line: 9},
	}

	weighted := MergeRRF(semantic, keyword, nil, nil, 10, DefaultRRFWeights())

	// Reference: build the exact same input lists, run plain RRF, compare scores.
	semIDs := []string{"a.go:Alpha", "b.go:Beta", "c.go:Gamma"}
	kwIDs := []string{"b.go:Beta", "d.go:Delta"}
	plain := rerank.RRF(rrfK, semIDs, kwIDs)

	plainScores := make(map[string]float64, len(plain))
	for _, f := range plain {
		plainScores[f.ID] = f.Score
	}

	if len(weighted) != len(plain) {
		t.Fatalf("len mismatch: weighted=%d plain=%d", len(weighted), len(plain))
	}
	for _, h := range weighted {
		key := h.FilePath + ":" + h.SymbolName
		want, ok := plainScores[key]
		if !ok {
			t.Errorf("weighted has %q, plain RRF doesn't", key)
			continue
		}
		if math.Abs(h.RRFScore-want) > 1e-12 {
			t.Errorf("score drift for %q: weighted=%.18f plain=%.18f",
				key, h.RRFScore, want)
		}
	}
}

// TestMergeRRFWeighted_SemanticHeavier verifies that a 2× semantic weight
// causes a semantic-only top-1 doc to outrank a keyword-only top-1 doc.
// Hand math: at rank 0 with k=60, 2.0/(60+1) = 0.03279 vs 1.0/(60+1) = 0.01639.
func TestMergeRRFWeighted_SemanticHeavier(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "sem.go", SymbolName: "OnlySem", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "kw.go", SymbolName: "OnlyKw", Line: 1},
	}

	results := MergeRRF(semantic, keyword, nil, nil, 10, RRFWeights{Semantic: 2.0, Keyword: 1.0})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].SymbolName != "OnlySem" {
		t.Errorf("expected semantic-only doc first under semantic-heavier weights, got %s", results[0].SymbolName)
	}
	if results[0].Source != "semantic" {
		t.Errorf("expected source=semantic for top result, got %s", results[0].Source)
	}
	if results[1].SymbolName != "OnlyKw" {
		t.Errorf("expected keyword-only doc second, got %s", results[1].SymbolName)
	}
}

// TestMergeRRFWeighted_KeywordHeavier is the symmetric companion: a 2× keyword
// weight flips the ranking so keyword-only top-1 outranks semantic-only top-1.
func TestMergeRRFWeighted_KeywordHeavier(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "sem.go", SymbolName: "OnlySem", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "kw.go", SymbolName: "OnlyKw", Line: 1},
	}

	results := MergeRRF(semantic, keyword, nil, nil, 10, RRFWeights{Semantic: 1.0, Keyword: 2.0})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].SymbolName != "OnlyKw" {
		t.Errorf("expected keyword-only doc first under keyword-heavier weights, got %s", results[0].SymbolName)
	}
	if results[0].Source != "keyword" {
		t.Errorf("expected source=keyword for top result, got %s", results[0].Source)
	}
	if results[1].SymbolName != "OnlySem" {
		t.Errorf("expected semantic-only doc second, got %s", results[1].SymbolName)
	}
}

// TestMergeRRFWeighted_NegativePanics mirrors go-kit/rerank.WeightedRRF's
// contract: negative weights are programmer errors, not runtime errors.
// MergeRRF must propagate the panic so misconfigured deploys fail loudly.
func TestMergeRRFWeighted_NegativePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on negative weight, got none")
		}
	}()

	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "A", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "B", Line: 1},
	}

	_ = MergeRRF(semantic, keyword, nil, nil, 10, RRFWeights{Semantic: -0.1, Keyword: 1.0})
}

// TestMergeRRF_EmptySparseArmIdentical is the load-bearing dark-launch safety
// test. It drives the real MergeRRF with a non-empty sparse arm but weight=0.0,
// and asserts that the fused ranking and scores are byte-identical to the 2-arm
// fusion. Source attribution is metadata and may differ (a doc also in the sparse
// arm's index map will show "hybrid" in source even at weight 0), but ranking must
// be unaffected.
//
// Why this proves the guarantee: WeightedRRF contribution of arm i to doc d is
// weight_i/(k+rank_i(d)). With weight_i=0 WeightedRRF explicitly skips the arm
// (confirmed in vendor: `if w == 0 { continue }`). Docs appearing only in the
// sparse arm cannot enter the fused output via score (0-contribution), so the
// ranked list of docs with positive scores is identical. The test verifies this
// on the REAL MergeRRF path (not reimplemented fusion), so it goes RED if a
// future change accidentally promotes zero-weight sparse docs into results.
func TestMergeRRF_EmptySparseArmIdentical(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "Alpha", Distance: 0.1},
		{FilePath: "b.go", SymbolName: "Beta", Distance: 0.2},
		{FilePath: "c.go", SymbolName: "Gamma", Distance: 0.3},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "Beta", Line: 5},
		{FilePath: "d.go", SymbolName: "Delta", Line: 9},
	}
	// A non-empty sparse arm. "Echo" would only surface via positive Sparse weight.
	// "Alpha" overlaps with semantic — its inSparse flag is set but its score is
	// unchanged (sparse contributes 0 at weight=0).
	sparseArm := []SparseHit{
		{FilePath: "e.go", SymbolName: "Echo", Line: 1},
		{FilePath: "a.go", SymbolName: "Alpha", Line: 3},
	}

	w := RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 0.0}

	// 2-arm reference (no sparse, no graph).
	ref := MergeRRF(semantic, keyword, nil, nil, 10, w)

	// 3-arm with non-empty sparse at weight 0.
	got := MergeRRF(semantic, keyword, sparseArm, nil, 10, w)

	// Ranking invariant: same count (sparse-only docs must NOT appear), same
	// order, same RRFScores. Source attribution may differ (metadata only).
	if len(got) != len(ref) {
		t.Fatalf("sparse arm at weight 0 changed result count: ref=%d got=%d\n"+
			"(sparse-only docs must not appear in output at weight=0)",
			len(ref), len(got))
	}
	for i := range ref {
		rKey := ref[i].FilePath + ":" + ref[i].SymbolName
		gKey := got[i].FilePath + ":" + got[i].SymbolName
		if rKey != gKey {
			t.Errorf("rank %d identity mismatch: ref=%q got=%q", i, rKey, gKey)
		}
		if math.Abs(ref[i].RRFScore-got[i].RRFScore) > 1e-12 {
			t.Errorf("rank %d score drift for %q: ref=%.18f got=%.18f",
				i, rKey, ref[i].RRFScore, got[i].RRFScore)
		}
	}
}

// TestMergeRRF_EmptyGraphArmIdentical is the Phase 1 graph-arm dark-launch safety
// test. Mirrors TestMergeRRF_EmptySparseArmIdentical exactly for the graph arm.
//
// Drives the real MergeRRF with a non-empty graph arm at weight=0.0 and asserts
// that the fused ranking and scores are byte-identical to the 3-arm (no-graph)
// result. This proves the dark-launch invariant: at RRF_WEIGHT_GRAPH=0 the arm is
// plumbed but contributes NOTHING to ranking.
//
// Falsification: setting weights.Graph = 0.5 in the second call causes GraphOnly
// to surface, making this test RED — confirming it actually guards the production
// behavior and would catch a weight-0 bypass.
func TestMergeRRF_EmptyGraphArmIdentical(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "Alpha", Distance: 0.1},
		{FilePath: "b.go", SymbolName: "Beta", Distance: 0.2},
		{FilePath: "c.go", SymbolName: "Gamma", Distance: 0.3},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "Beta", Line: 5},
		{FilePath: "d.go", SymbolName: "Delta", Line: 9},
	}
	// A non-empty graph arm with one unique doc and one overlap.
	// "GraphOnly" would only surface if Graph weight > 0.
	// "Alpha" overlaps with semantic — inGraph is set but score unchanged at weight=0.
	graphArm := []GraphHit{
		{FilePath: "g.go", SymbolName: "GraphOnly"},
		{FilePath: "a.go", SymbolName: "Alpha"},
	}

	w := RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 0.0, Graph: 0.0}

	// 3-arm reference (no graph).
	ref := MergeRRF(semantic, keyword, nil, nil, 10, w)

	// 4-arm with non-empty graph at weight 0.
	got := MergeRRF(semantic, keyword, nil, graphArm, 10, w)

	// Ranking invariant: same count (graph-only docs must NOT appear), same
	// order, same RRFScores. Source attribution is metadata and may differ.
	if len(got) != len(ref) {
		t.Fatalf("graph arm at weight 0 changed result count: ref=%d got=%d\n"+
			"(graph-only docs must not appear in output at weight=0)",
			len(ref), len(got))
	}
	for i := range ref {
		rKey := ref[i].FilePath + ":" + ref[i].SymbolName
		gKey := got[i].FilePath + ":" + got[i].SymbolName
		if rKey != gKey {
			t.Errorf("rank %d identity mismatch: ref=%q got=%q", i, rKey, gKey)
		}
		if math.Abs(ref[i].RRFScore-got[i].RRFScore) > 1e-12 {
			t.Errorf("rank %d score drift for %q: ref=%.18f got=%.18f",
				i, rKey, ref[i].RRFScore, got[i].RRFScore)
		}
	}
}

// TestMergeRRF_GraphContributesWhenWeightPositive verifies the positive case:
// a doc found ONLY by the graph arm surfaces in fused results when Graph=0.5,
// and does NOT appear when Graph=0.0.
//
// Falsification: use Graph=0.0 in the "active" call — GraphOnly will NOT surface,
// making this test RED and confirming it tracks the production guard.
func TestMergeRRF_GraphContributesWhenWeightPositive(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "sem.go", SymbolName: "SemOnly", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "kw.go", SymbolName: "KwOnly", Line: 1},
	}
	// GraphOnly is unique to the graph arm.
	graphArm := []GraphHit{
		{FilePath: "gr.go", SymbolName: "GraphOnly"},
	}

	// With Graph=0.0: GraphOnly must NOT appear.
	darkLaunch := MergeRRF(semantic, keyword, nil, graphArm, 10, RRFWeights{Semantic: 1.0, Keyword: 1.0, Graph: 0.0})
	for _, r := range darkLaunch {
		if r.SymbolName == "GraphOnly" {
			t.Errorf("Graph=0.0: GraphOnly must not appear, but got source=%q", r.Source)
		}
	}

	// With Graph=0.5: GraphOnly MUST appear and carry source="graph".
	active := MergeRRF(semantic, keyword, nil, graphArm, 10, RRFWeights{Semantic: 1.0, Keyword: 1.0, Graph: 0.5})
	found := false
	for _, r := range active {
		if r.SymbolName == "GraphOnly" {
			found = true
			if r.Source != "graph" {
				t.Errorf("Graph=0.5: GraphOnly source want %q got %q", "graph", r.Source)
			}
		}
	}
	if !found {
		t.Error("Graph=0.5: GraphOnly did not surface in fused results")
	}
}

// TestMergeRRF_SparseContributesWhenWeightPositive verifies the positive case:
// a doc found ONLY by the sparse arm surfaces in fused results when Sparse=0.5,
// and does NOT appear when Sparse=0.0.
//
// This is a falsifiable test: revert weights.Sparse from 0.0 to 0.5 in the
// second call and the "SparseOnly" should NOT surface — confirming the test
// accurately tracks the production guard.
func TestMergeRRF_SparseContributesWhenWeightPositive(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "sem.go", SymbolName: "SemOnly", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "kw.go", SymbolName: "KwOnly", Line: 1},
	}
	// SparseOnly is unique to the sparse arm.
	sparseArm := []SparseHit{
		{FilePath: "sp.go", SymbolName: "SparseOnly", Line: 7},
	}

	// With Sparse=0.0: SparseOnly must NOT appear.
	darkLaunch := MergeRRF(semantic, keyword, sparseArm, nil, 10, RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 0.0})
	for _, r := range darkLaunch {
		if r.SymbolName == "SparseOnly" {
			t.Errorf("Sparse=0.0: SparseOnly must not appear, but got source=%q", r.Source)
		}
	}

	// With Sparse=0.5: SparseOnly MUST appear and carry source="sparse".
	active := MergeRRF(semantic, keyword, sparseArm, nil, 10, RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 0.5})
	found := false
	for _, r := range active {
		if r.SymbolName == "SparseOnly" {
			found = true
			if r.Source != "sparse" {
				t.Errorf("Sparse=0.5: SparseOnly source want %q got %q", "sparse", r.Source)
			}
		}
	}
	if !found {
		t.Error("Sparse=0.5: SparseOnly did not surface in fused results")
	}
}

// TestMergeRRF_SparseNilClientIdentical verifies that passing a nil sparse arm
// produces the same output as passing an empty slice — the nil-client gate in
// handleSemanticHits is safe.
func TestMergeRRF_SparseNilClientIdentical(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "A", Distance: 0.1},
		{FilePath: "b.go", SymbolName: "B", Distance: 0.2},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "B", Line: 3},
	}

	w := RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 1.0}

	withNil := MergeRRF(semantic, keyword, nil, nil, 10, w)
	withEmpty := MergeRRF(semantic, keyword, []SparseHit{}, nil, 10, w)

	if len(withNil) != len(withEmpty) {
		t.Fatalf("nil vs empty sparse: count mismatch nil=%d empty=%d", len(withNil), len(withEmpty))
	}
	for i := range withNil {
		if withNil[i].SymbolName != withEmpty[i].SymbolName {
			t.Errorf("rank %d: nil=%q empty=%q", i, withNil[i].SymbolName, withEmpty[i].SymbolName)
		}
		if math.Abs(withNil[i].RRFScore-withEmpty[i].RRFScore) > 1e-12 {
			t.Errorf("rank %d score drift: nil=%.18f empty=%.18f",
				i, withNil[i].RRFScore, withEmpty[i].RRFScore)
		}
	}
}

// TestMergeRRF_SparseThreeWayHybrid confirms that a doc appearing in all three
// arms carries source="hybrid".
func TestMergeRRF_SparseThreeWayHybrid(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "x.go", SymbolName: "Tri", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "x.go", SymbolName: "Tri", Line: 5},
	}
	sparseArm := []SparseHit{
		{FilePath: "x.go", SymbolName: "Tri", Line: 5},
	}

	results := MergeRRF(semantic, keyword, sparseArm, nil, 10,
		RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 1.0})

	if len(results) != 1 {
		t.Fatalf("expected 1 deduped result, got %d", len(results))
	}
	if results[0].SymbolName != "Tri" {
		t.Fatalf("expected Tri, got %s", results[0].SymbolName)
	}
	if results[0].Source != "hybrid" {
		t.Errorf("three-way overlap: expected source=hybrid, got %q", results[0].Source)
	}
}

// TestMergeRRF_FourWayHybrid confirms that a doc appearing in all four arms
// (semantic + keyword + sparse + graph) carries source="hybrid".
func TestMergeRRF_FourWayHybrid(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "x.go", SymbolName: "Quad", Distance: 0.1},
	}
	keyword := []KeywordHit{
		{FilePath: "x.go", SymbolName: "Quad", Line: 5},
	}
	sparseArm := []SparseHit{
		{FilePath: "x.go", SymbolName: "Quad", Line: 5},
	}
	graphArm := []GraphHit{
		{FilePath: "x.go", SymbolName: "Quad"},
	}

	results := MergeRRF(semantic, keyword, sparseArm, graphArm, 10,
		RRFWeights{Semantic: 1.0, Keyword: 1.0, Sparse: 1.0, Graph: 1.0})

	if len(results) != 1 {
		t.Fatalf("expected 1 deduped result, got %d", len(results))
	}
	if results[0].SymbolName != "Quad" {
		t.Fatalf("expected Quad, got %s", results[0].SymbolName)
	}
	if results[0].Source != "hybrid" {
		t.Errorf("four-way overlap: expected source=hybrid, got %q", results[0].Source)
	}
}
