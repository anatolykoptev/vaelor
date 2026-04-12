package codegraph

import "testing"

func TestScoreSurprise_CrossPackage(t *testing.T) {
	edge := surpriseEdge{
		FromName: "Foo", FromFile: "pkg/a/handler.go", FromPkg: "a",
		ToName: "Bar", ToFile: "pkg/b/store.go", ToPkg: "b",
		EdgeLabel:     "CALLS",
		FromCommunity: 0, ToCommunity: 0,
		FromDegree: 5, ToDegree: 5,
		FromPageRank: 0.01, ToPageRank: 0.01,
	}
	score, reasons := scoreSurprise(edge)
	if score < 2 {
		t.Errorf("cross-package should score ≥2, got %d", score)
	}
	found := false
	for _, r := range reasons {
		if r == "crosses package boundary (a → b)" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cross-package reason, got %v", reasons)
	}
}

func TestScoreSurprise_CrossCommunity(t *testing.T) {
	edge := surpriseEdge{
		FromName: "Foo", FromFile: "pkg/a/a.go", FromPkg: "a",
		ToName: "Bar", ToFile: "pkg/a/b.go", ToPkg: "a",
		EdgeLabel:     "CALLS",
		FromCommunity: 0, ToCommunity: 3,
		FromDegree: 5, ToDegree: 5,
		FromPageRank: 0.01, ToPageRank: 0.01,
	}
	score, _ := scoreSurprise(edge)
	if score < 1 {
		t.Errorf("cross-community should score ≥1, got %d", score)
	}
}

func TestScoreSurprise_PeripheralToHub(t *testing.T) {
	edge := surpriseEdge{
		FromName: "tiny", FromFile: "pkg/a/a.go", FromPkg: "a",
		ToName: "BigHub", ToFile: "pkg/a/b.go", ToPkg: "a",
		EdgeLabel:     "CALLS",
		FromCommunity: 0, ToCommunity: 0,
		FromDegree: 1, ToDegree: 20,
		FromPageRank: 0.001, ToPageRank: 0.05,
	}
	score, _ := scoreSurprise(edge)
	if score < 1 {
		t.Errorf("peripheral→hub should score ≥1, got %d", score)
	}
}

func TestScoreSurprise_PageRankGap(t *testing.T) {
	edge := surpriseEdge{
		FromName: "obscure", FromFile: "pkg/a/a.go", FromPkg: "a",
		ToName: "core", ToFile: "pkg/b/b.go", ToPkg: "b",
		EdgeLabel:     "CALLS",
		FromCommunity: 0, ToCommunity: 1,
		FromDegree: 3, ToDegree: 8,
		FromPageRank: 0.0005, ToPageRank: 0.08,
	}
	score, _ := scoreSurprise(edge)
	// Cross-package(2) + cross-community(1) + PageRank gap(1) + cross-file(1) = 5
	if score < 4 {
		t.Errorf("expected ≥4, got %d", score)
	}
}

func TestRankSurprises_TopN(t *testing.T) {
	edges := []surpriseEdge{
		{FromName: "a", FromFile: "x/a.go", FromPkg: "x", ToName: "b", ToFile: "y/b.go", ToPkg: "y", FromCommunity: 0, ToCommunity: 1, FromDegree: 5, ToDegree: 5},
		{FromName: "c", FromFile: "x/c.go", FromPkg: "x", ToName: "d", ToFile: "x/d.go", ToPkg: "x", FromCommunity: 0, ToCommunity: 0, FromDegree: 5, ToDegree: 5},
	}
	results := rankSurprises(edges, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Score < results[1].Score {
		t.Error("results should be sorted by score descending")
	}
}
