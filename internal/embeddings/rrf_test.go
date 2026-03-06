package embeddings

import (
	"testing"
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

	results := MergeRRF(semantic, keyword, 10)

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

	results := MergeRRF(nil, keyword, 10)

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

	results := MergeRRF(semantic, nil, 10)

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

	results := MergeRRF(semantic, nil, 5)

	if len(results) != 5 {
		t.Errorf("expected exactly 5 results with topK=5, got %d", len(results))
	}
}

func TestRRFMergeBothEmpty(t *testing.T) {
	results := MergeRRF(nil, nil, 10)
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}
