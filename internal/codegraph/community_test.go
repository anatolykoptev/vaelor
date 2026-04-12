package codegraph

import "testing"

func TestInjectCommunities_SetsPropertyOnSymbols(t *testing.T) {
	vertices := []vertexData{
		{Label: "File", Props: map[string]string{"path": "main.go"}},
		{Label: "Symbol", Props: map[string]string{"name": "Foo", "file": "main.go"}},
		{Label: "Symbol", Props: map[string]string{"name": "Bar", "file": "main.go"}},
		{Label: "Symbol", Props: map[string]string{"name": "Baz", "file": "util.go"}},
		{Label: "Package", Props: map[string]string{"path": "."}},
	}
	edges := []edgeData{
		{FromLabel: "Symbol", FromKey: "Foo:main.go", ToLabel: "Symbol", ToKey: "Bar:main.go", EdgeLabel: "CALLS"},
		{FromLabel: "Symbol", FromKey: "Foo:main.go", ToLabel: "Symbol", ToKey: "Baz:util.go", EdgeLabel: "CALLS"},
	}

	injectCommunities(vertices, edges)

	for _, v := range vertices {
		if v.Label != "Symbol" {
			continue
		}
		if _, ok := v.Props["community"]; !ok {
			t.Errorf("symbol %s missing community property", v.Props["name"])
		}
	}
}

func TestInjectCommunities_NoEdges(t *testing.T) {
	vertices := []vertexData{
		{Label: "Symbol", Props: map[string]string{"name": "Lone", "file": "a.go"}},
	}
	injectCommunities(vertices, nil)
	if _, ok := vertices[0].Props["community"]; !ok {
		t.Error("expected community property on isolated symbol")
	}
}

func TestInjectCommunities_SkipsNonSymbols(t *testing.T) {
	vertices := []vertexData{
		{Label: "File", Props: map[string]string{"path": "main.go"}},
		{Label: "Package", Props: map[string]string{"path": "."}},
	}
	injectCommunities(vertices, nil)
	for _, v := range vertices {
		if _, ok := v.Props["community"]; ok {
			t.Errorf("%s should not have community property", v.Label)
		}
	}
}
