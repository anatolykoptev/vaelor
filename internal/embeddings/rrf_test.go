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

	results := MergeRRF(semantic, keyword, 10, DefaultRRFWeights())

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

	results := MergeRRF(nil, keyword, 10, DefaultRRFWeights())

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

	results := MergeRRF(semantic, nil, 10, DefaultRRFWeights())

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

	results := MergeRRF(semantic, nil, 5, DefaultRRFWeights())

	if len(results) != 5 {
		t.Errorf("expected exactly 5 results with topK=5, got %d", len(results))
	}
}

func TestRRFMergeBothEmpty(t *testing.T) {
	results := MergeRRF(nil, nil, 10, DefaultRRFWeights())
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

	weighted := MergeRRF(semantic, keyword, 10, DefaultRRFWeights())

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

	results := MergeRRF(semantic, keyword, 10, RRFWeights{Semantic: 2.0, Keyword: 1.0})

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

	results := MergeRRF(semantic, keyword, 10, RRFWeights{Semantic: 1.0, Keyword: 2.0})

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

	_ = MergeRRF(semantic, keyword, 10, RRFWeights{Semantic: -0.1, Keyword: 1.0})
}
